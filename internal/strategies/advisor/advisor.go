// Package advisor periodically asks the AI planner for a portfolio review.
// Suggestions are printed and journaled; nothing is executed automatically.
package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/doedja/hibachi-go/ws"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/memory"
	"github.com/doedja/hibachi-cli/internal/strategies"
)

func init() {
	strategies.Register("advisor", func() strategies.Strategy { return New() })
}

type Strategy struct {
	every      time.Duration
	symbolsCSV string
	symbols    []string
	invocation string
}

func New() *Strategy { return &Strategy{} }

func (s *Strategy) Name() string { return "advisor" }

func (s *Strategy) Description() string {
	return "advisor: periodic AI portfolio review; prints suggestions, never executes"
}

func (s *Strategy) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("advisor", flag.ContinueOnError)
	var everyStr string
	fs.StringVar(&everyStr, "every", "5m", "time between ticks (Go duration)")
	fs.StringVar(&s.symbolsCSV, "symbols", "", "comma-separated symbols; defaults to open positions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	d, err := time.ParseDuration(everyStr)
	if err != nil {
		return fmt.Errorf("invalid --every: %w", err)
	}
	if d <= 0 {
		return fmt.Errorf("--every must be positive")
	}
	s.every = d
	if s.symbolsCSV != "" {
		for _, raw := range strings.Split(s.symbolsCSV, ",") {
			sym := strings.TrimSpace(raw)
			if sym != "" {
				s.symbols = append(s.symbols, sym)
			}
		}
	}
	return nil
}

func (s *Strategy) Run(ctx context.Context, deps strategies.AgentDeps) error {
	if deps.Planner == nil {
		return errors.New("advisor requires an AI backend; ensure ai.backend is set")
	}
	log := deps.Logger
	if log == nil {
		log = func(string) {}
	}
	s.invocation = fmt.Sprintf("advisor:%d", time.Now().Unix())

	sessionsPath := sessionRegistryPath(deps.Cfg.Journal.Path)
	sessionID := loadSessionID(sessionsPath, s.invocation)

	log(fmt.Sprintf("advisor running: every=%s symbols=%v session=%s", s.every, s.symbols, s.invocation))

	// First tick immediately so the user gets feedback without waiting.
	firstTick := true
	if err := s.tick(ctx, deps, log, &sessionID, firstTick); err != nil {
		log(fmt.Sprintf("tick error: %v", err))
	}
	firstTick = false
	_ = saveSessionID(sessionsPath, s.invocation, sessionID)

	t := time.NewTicker(s.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log("advisor stopped")
			return nil
		case <-t.C:
			if err := s.tick(ctx, deps, log, &sessionID, firstTick); err != nil {
				log(fmt.Sprintf("tick error: %v", err))
			}
			_ = saveSessionID(sessionsPath, s.invocation, sessionID)
		}
	}
}

func (s *Strategy) tick(
	ctx context.Context, deps strategies.AgentDeps, log func(string),
	sessionID *string, firstTick bool,
) error {
	payload, syms, err := s.buildPayload(ctx, deps)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	sys, err := aiagent.SystemPrompt("advisor")
	if err != nil {
		return fmt.Errorf("system prompt: %w", err)
	}

	resp, err := deps.Planner.Plan(ctx, aiagent.Request{
		SessionID:    *sessionID,
		SystemPrompt: sys,
		UserPayload:  payload,
		Fresh:        firstTick,
	})
	if err != nil {
		return fmt.Errorf("planner: %w", err)
	}
	*sessionID = resp.SessionID

	var plan aiagent.Plan
	if err := json.Unmarshal(resp.Content, &plan); err != nil {
		return fmt.Errorf("decode plan: %w", err)
	}

	if plan.Reasoning != "" {
		log("reasoning: " + plan.Reasoning)
	}
	if len(plan.Actions) == 0 {
		log("no suggestions this tick")
	}
	for i, a := range plan.Actions {
		log(fmt.Sprintf("suggestion %d: %s %s %s qty=%s price=%s reason=%s",
			i+1, a.Kind, a.Symbol, a.Side, a.Qty, a.Price, a.Reason))
	}
	if plan.Ask != "" {
		log("advisor asks: " + plan.Ask)
	}

	ev := map[string]any{
		"suggestions": plan.Actions,
		"reasoning":   plan.Reasoning,
		"ask":         plan.Ask,
		"cost_usd":    resp.CostUSD,
		"tokens_in":   resp.TokensIn,
		"tokens_out":  resp.TokensOut,
		"session_id":  resp.SessionID,
		"symbols":     syms,
	}
	raw, _ := json.Marshal(ev)
	_, _ = deps.Journal.Record(ctx, journal.Event{
		Kind:    "advisor_tick",
		Agent:   s.invocation,
		Payload: raw,
	})

	if deps.Memory != nil {
		for _, w := range plan.MemoryWrites {
			if err := deps.Memory.Write(w.File, w.Content); err != nil {
				log(fmt.Sprintf("memory write %s: %v", w.File, err))
			}
		}
		for _, name := range plan.MemoryDeletes {
			if err := deps.Memory.Delete(name); err != nil {
				log(fmt.Sprintf("memory delete %s: %v", name, err))
			}
		}
	}
	return nil
}

func (s *Strategy) buildPayload(ctx context.Context, deps strategies.AgentDeps) (json.RawMessage, []string, error) {
	// Memory.
	memBody := ""
	if deps.Memory != nil {
		if body, err := deps.Memory.ReadAll(); err == nil {
			memBody = body
		}
	}

	// Contracts.
	var contractsRaw json.RawMessage
	info, err := deps.Client.GetExchangeInfo(ctx)
	if err == nil && info != nil {
		if b, merr := json.Marshal(info.FutureContracts); merr == nil {
			contractsRaw = b
		}
	}

	// Account + positions + pending orders.
	account := map[string]any{}
	if ai, err := deps.Client.GetAccountInfo(ctx); err == nil && ai != nil {
		account["info"] = ai
	}
	if bal, err := deps.Client.GetCapitalBalance(ctx); err == nil && bal != nil {
		account["balance"] = bal
	}
	if ords, err := deps.Client.GetPendingOrders(ctx); err == nil {
		account["pending_orders"] = ords
	}

	var positions []hibachi.Position
	if deps.Cfg.API.APIKey != "" && deps.Cfg.API.AccountID != 0 {
		snapCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if pos, err := fetchPositions(snapCtx, deps); err == nil {
			positions = pos
			account["positions"] = pos
		}
		cancel()
	}

	// Resolve symbols: explicit flag, else from open positions.
	syms := s.symbols
	if len(syms) == 0 {
		for _, p := range positions {
			if p.Symbol != "" {
				syms = append(syms, p.Symbol)
			}
		}
	}

	// Per-symbol market snapshot.
	market := map[string]any{}
	for _, sym := range syms {
		if pr, err := deps.Client.GetPrices(ctx, sym); err == nil && pr != nil {
			market[sym] = pr
		}
	}

	// Recent journal outcomes for context (last 24h, filtered by symbol if set).
	recent := collectRecentOutcomes(ctx, deps.Journal, syms)
	if len(recent) > 0 {
		account["recent_outcomes"] = recent
	}

	var accountRaw, marketRaw json.RawMessage
	if len(account) > 0 {
		if b, err := json.Marshal(account); err == nil {
			accountRaw = b
		}
	}
	if len(market) > 0 {
		if b, err := json.Marshal(market); err == nil {
			marketRaw = b
		}
	}

	raw, err := aiagent.BuildPayload(aiagent.ContextInput{
		Trigger:    "advisor_tick",
		UserPrompt: "advisor: periodic portfolio review; suggest adjustments if warranted",
		Memory:     memBody,
		Account:    accountRaw,
		Market:     marketRaw,
		Contracts:  contractsRaw,
	})
	return raw, syms, err
}

func fetchPositions(ctx context.Context, deps strategies.AgentDeps) ([]hibachi.Position, error) {
	client := ws.NewAccountClient(ws.AccountClientOptions{
		APIKey:    deps.Cfg.API.APIKey,
		AccountID: deps.Cfg.API.AccountID,
	})
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect account ws: %w", err)
	}
	defer client.Disconnect()
	res, err := client.StreamStart(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream start: %w", err)
	}
	return res.AccountSnapshot.Positions, nil
}

func collectRecentOutcomes(ctx context.Context, j *journal.Journal, symbols []string) []journal.Event {
	if j == nil {
		return nil
	}
	since := time.Now().Add(-24 * time.Hour)
	events, err := j.Since(ctx, since)
	if err != nil {
		return nil
	}
	if len(symbols) == 0 {
		if len(events) > 20 {
			events = events[len(events)-20:]
		}
		return events
	}
	want := map[string]bool{}
	for _, s := range symbols {
		want[s] = true
	}
	var out []journal.Event
	for _, e := range events {
		if e.Symbol == "" || want[e.Symbol] {
			out = append(out, e)
		}
	}
	if len(out) > 20 {
		out = out[len(out)-20:]
	}
	return out
}

// sessionRegistryPath keeps the advisor session id next to the journal.
func sessionRegistryPath(journalPath string) string {
	return filepath.Join(filepath.Dir(journalPath), "advisor-sessions.json")
}

type advisorSession struct {
	SessionID string    `json:"session_id"`
	Updated   time.Time `json:"updated"`
}

func loadSessionID(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	reg := map[string]*advisorSession{}
	if err := json.Unmarshal(data, &reg); err != nil {
		return ""
	}
	if e, ok := reg[key]; ok && e != nil {
		return e.SessionID
	}
	return ""
}

func saveSessionID(path, key, sessionID string) error {
	reg := map[string]*advisorSession{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &reg)
	}
	reg[key] = &advisorSession{SessionID: sessionID, Updated: time.Now().UTC()}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// MemoryStorePlaceholder is a tiny interface to document what we use from
// memory.Store; helpful only for keeping imports tidy.
var _ memoryStoreAPI = (*memory.Store)(nil)

type memoryStoreAPI interface {
	ReadAll() (string, error)
	Write(name, content string) error
	Delete(name string) error
}
