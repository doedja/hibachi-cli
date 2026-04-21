package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/config"
	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "hibachi-cli"
	keyringUser    = "openrouter-api-key"
)

// resolveAPIKey tries the OS keychain first (stored by `hibachi ai keys set
// openrouter`), then falls back to the configured env var. Keychain wins so
// users who ran `keys set` don't also need to export the env var.
func resolveAPIKey(envName string) string {
	if k, err := keyring.Get(keyringService, keyringUser); err == nil && k != "" {
		return k
	}
	return os.Getenv(envName)
}

type Planner struct {
	cfg    config.OpenRouterConfig
	client *http.Client
}

func New(cfg config.OpenRouterConfig) (*Planner, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = "OPENROUTER_API_KEY"
	}
	return &Planner{cfg: cfg, client: &http.Client{Timeout: timeout}}, nil
}

func (p *Planner) Backend() string { return "openrouter" }
func (p *Planner) Model() string   { return p.cfg.Model }
func (p *Planner) Close() error    { return nil }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string                 `json:"model"`
	Messages       []chatMessage          `json:"messages"`
	ResponseFormat map[string]string      `json:"response_format,omitempty"`
	Temperature    float64                `json:"temperature,omitempty"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	Extra          map[string]interface{} `json:"-"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalCost        float64 `json:"cost"`
	} `json:"usage"`
	// OpenRouter sometimes surfaces cost at the top level as well.
	XCostTotal float64 `json:"x_cost_total"`
}

func (p *Planner) Plan(ctx context.Context, req aiagent.Request) (*aiagent.Response, error) {
	apiKey := resolveAPIKey(p.cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindUnauthorized, Message: fmt.Sprintf("no openrouter key (set via `hibachi ai keys set openrouter` or export %s)", p.cfg.APIKeyEnv)}
	}

	sessionID := req.SessionID
	if sessionID == "" || req.Fresh {
		sessionID = uuid.NewString()
	}

	priorMsgs, err := readTranscript(sessionID, req.Fresh)
	if err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "read transcript", Err: err}
	}

	msgs := make([]chatMessage, 0, len(priorMsgs)+2)
	msgs = append(msgs, chatMessage{Role: "system", Content: req.SystemPrompt})
	msgs = append(msgs, priorMsgs...)
	msgs = append(msgs, chatMessage{Role: "user", Content: string(req.UserPayload)})

	body := chatRequest{
		Model:          p.cfg.Model,
		Messages:       msgs,
		ResponseFormat: map[string]string{"type": "json_object"},
		Temperature:    p.cfg.Temperature,
		MaxTokens:      p.cfg.MaxTokens,
	}
	if body.Temperature == 0 {
		body.Temperature = 0.3
	}
	if body.MaxTokens == 0 {
		body.MaxTokens = 4096
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "marshal request", Err: err}
	}

	endpoint := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "build request", Err: err}
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &aiagent.PlanError{Kind: aiagent.ErrKindTimeout, Message: "openrouter timeout", Err: err}
		}
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindUnavailable, Message: "openrouter network", Err: err}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "read body", Err: err}
	}
	if resp.StatusCode != 200 {
		kind := aiagent.ErrKindUnavailable
		switch resp.StatusCode {
		case 401, 403:
			kind = aiagent.ErrKindUnauthorized
		case 429:
			kind = aiagent.ErrKindRateLimited
		}
		return nil, &aiagent.PlanError{Kind: kind, Message: fmt.Sprintf("http %d: %s", resp.StatusCode, truncate(string(raw), 400))}
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "parse response", Err: err}
	}
	if len(parsed.Choices) == 0 {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "no choices returned"}
	}
	content := parsed.Choices[0].Message.Content
	contentJSON, err := extractJSON(content)
	if err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "extract plan json", Err: err}
	}

	// Persist transcript only on success so failures don't poison history.
	if err := appendTranscript(sessionID, chatMessage{Role: "user", Content: string(req.UserPayload)}); err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "write transcript", Err: err}
	}
	if err := appendTranscript(sessionID, chatMessage{Role: "assistant", Content: content}); err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "write transcript", Err: err}
	}

	cost := parsed.Usage.TotalCost
	if cost == 0 {
		cost = parsed.XCostTotal
	}

	return &aiagent.Response{
		SessionID: sessionID,
		Content:   contentJSON,
		TokensIn:  parsed.Usage.PromptTokens,
		TokensOut: parsed.Usage.CompletionTokens,
		CostUSD:   cost,
		RawText:   content,
	}, nil
}

func transcriptPath(id string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".hibachi", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".jsonl"), nil
}

func readTranscript(id string, fresh bool) ([]chatMessage, error) {
	if fresh {
		return nil, nil
	}
	path, err := transcriptPath(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var out []chatMessage
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m chatMessage
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func appendTranscript(id string, m chatMessage) error {
	path, err := transcriptPath(id)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

func extractJSON(text string) (json.RawMessage, error) {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	var any interface{}
	if err := json.Unmarshal([]byte(s), &any); err != nil {
		return nil, err
	}
	return json.RawMessage(s), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
