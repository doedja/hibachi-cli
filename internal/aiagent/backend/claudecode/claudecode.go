package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/config"
	"github.com/google/uuid"
)

type Planner struct {
	bin     string
	model   string
	timeout time.Duration
}

func New(cfg config.ClaudeCodeConfig) (*Planner, error) {
	bin := cfg.Bin
	if bin == "" {
		bin = "claude"
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &Planner{bin: bin, model: cfg.Model, timeout: timeout}, nil
}

func (p *Planner) Backend() string { return "claude-code" }
func (p *Planner) Model() string   { return p.model }
func (p *Planner) Close() error    { return nil }

// claudeJSONResult matches the envelope that `claude -p --output-format json`
// emits on stdout when it finishes.
type claudeJSONResult struct {
	SessionID    string  `json:"session_id"`
	NumTurns     int     `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (p *Planner) Plan(ctx context.Context, req aiagent.Request) (*aiagent.Response, error) {
	sessionID := req.SessionID
	if sessionID == "" || req.Fresh {
		sessionID = uuid.NewString()
	}

	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	args := []string{
		"-p",
		"--output-format", "json",
		"--system-prompt", req.SystemPrompt,
	}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	if req.Fresh || req.SessionID == "" {
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--resume", sessionID)
	}

	cmd := exec.CommandContext(cctx, p.bin, args...)
	cmd.Stdin = bytes.NewReader(req.UserPayload)

	// Strip CLAUDECODE so a nested invocation doesn't inherit the parent
	// Code harness project context.
	env := os.Environ()
	filtered := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_") {
			continue
		}
		filtered = append(filtered, kv)
	}
	cmd.Env = filtered

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return nil, &aiagent.PlanError{Kind: aiagent.ErrKindTimeout, Message: "claude code timed out", Err: err}
		}
		return nil, &aiagent.PlanError{
			Kind:    aiagent.ErrKindUnavailable,
			Message: fmt.Sprintf("claude code failed: %s", strings.TrimSpace(stderr.String())),
			Err:     err,
		}
	}

	var env_ claudeJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &env_); err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "parse claude envelope", Err: err}
	}
	if env_.IsError {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: env_.Result}
	}

	content, err := extractJSON(env_.Result)
	if err != nil {
		return nil, &aiagent.PlanError{Kind: aiagent.ErrKindBadResponse, Message: "extract plan json", Err: err}
	}

	return &aiagent.Response{
		SessionID: env_.SessionID,
		Content:   content,
		NumTurns:  env_.NumTurns,
		TokensIn:  env_.Usage.InputTokens,
		TokensOut: env_.Usage.OutputTokens,
		CostUSD:   env_.TotalCostUSD,
		RawText:   env_.Result,
	}, nil
}

// extractJSON pulls a JSON object out of assistant text. Claude sometimes
// wraps it in ```json fences; we peel those off when present.
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
	// Validate it parses as JSON.
	var any interface{}
	if err := json.Unmarshal([]byte(s), &any); err != nil {
		return nil, err
	}
	return json.RawMessage(s), nil
}
