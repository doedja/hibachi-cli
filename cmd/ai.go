package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
	"golang.org/x/term"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/aiagent/backend"
	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/config"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/memory"
	"github.com/doedja/hibachi-cli/internal/output"
	"github.com/doedja/hibachi-cli/internal/safety"
)

const (
	sessionIdleExpiry = 30 * time.Minute

	keyringServiceName = "hibachi-cli"

	sessionOneshot = "oneshot"
)

var modelPresets = []string{
	"claude-code:claude-opus-4-7",
	"openrouter:anthropic/claude-opus-4.7",
	"openrouter:anthropic/claude-sonnet-4.6",
	"openrouter:openai/gpt-5",
	"openrouter:google/gemini-2.5-pro",
}

func newAICmd() *cobra.Command {
	var fresh bool
	c := &cobra.Command{
		Use:   "ai [prompt...]",
		Short: "Natural-language trading via Claude or OpenRouter",
		Long:  "Sends your words to the configured AI backend, previews a plan, confirms, then executes via the SDK.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.TrimSpace(strings.Join(args, " "))
			if prompt == "" {
				return cmd.Help()
			}
			return runAIOneShot(cmd, prompt, fresh)
		},
	}
	c.Flags().BoolVar(&fresh, "fresh", false, "start a new AI session instead of resuming")
	c.AddCommand(
		newAIChatCmd(),
		newAIBackendCmd(),
		newAIModelsCmd(),
		newAIKeysCmd(),
		newAIUsageCmd(),
		newAISessionsCmd(),
	)
	return c
}

// runAIOneShot drives a single plan->confirm->execute cycle.
func runAIOneShot(cmd *cobra.Command, prompt string, fresh bool) error {
	a := app.From(cmd.Context())
	if err := a.EnsureClient(); err != nil {
		return err
	}

	planner, err := backend.New(a.Cfg)
	if err != nil {
		return err
	}
	defer planner.Close()

	ctx := cmd.Context()
	currentPrompt := prompt

	// Resolve or create session.
	regPath := sessionsPath(a.Cfg.Journal.Path)
	registry, err := loadSessions(regPath)
	if err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}
	entry := pickSession(registry, sessionOneshot, planner, fresh)
	freshCall := fresh || entry.SessionID == ""

	for attempt := 0; attempt < 3; attempt++ {
		plan, resp, err := planOnce(ctx, a, planner, currentPrompt, "user-prompt", entry.SessionID, freshCall)
		if err != nil {
			return err
		}
		freshCall = false
		entry.SessionID = resp.SessionID
		entry.Backend = planner.Backend()
		entry.Model = planner.Model()
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now().UTC()
		}
		entry.LastUsed = time.Now().UTC()
		entry.Turns += 1 + resp.NumTurns
		entry.CostUSD += resp.CostUSD

		if plan.Ask != "" && attempt < 2 {
			fmt.Println(color.New(color.FgYellow).Sprint("Question: ") + plan.Ask)
			// If stdin isn't a terminal we can't prompt; print the ask and exit.
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				registry[sessionOneshot] = entry
				_ = saveSessions(regPath, registry)
				return nil
			}
			fmt.Print("> ")
			reader := bufio.NewReader(os.Stdin)
			line, rerr := reader.ReadString('\n')
			if rerr != nil {
				// EOF or interrupt: finalise without failing.
				registry[sessionOneshot] = entry
				_ = saveSessions(regPath, registry)
				return nil
			}
			currentPrompt = currentPrompt + "\n" + strings.TrimSpace(line)
			continue
		}

		// Finalise: save registry updates even if we stop here.
		registry[sessionOneshot] = entry
		if err := saveSessions(regPath, registry); err != nil {
			fmt.Fprintf(os.Stderr, "warn: save sessions: %v\n", err)
		}

		return finishPlan(ctx, a, plan, resp)
	}
	return errors.New("too many clarification rounds; aborting")
}

// planOnce builds the context, calls the planner, and parses the plan.
func planOnce(
	ctx context.Context,
	a *app.App,
	planner aiagent.Planner,
	prompt, trigger, sessionID string,
	fresh bool,
) (aiagent.Plan, *aiagent.Response, error) {
	payload, err := buildContextPayload(ctx, a, prompt, trigger)
	if err != nil {
		return aiagent.Plan{}, nil, err
	}

	sys, err := aiagent.SystemPrompt("oneshot")
	if err != nil {
		return aiagent.Plan{}, nil, err
	}

	resp, err := planner.Plan(ctx, aiagent.Request{
		SessionID:    sessionID,
		SystemPrompt: sys,
		UserPayload:  payload,
		Fresh:        fresh,
	})
	if err != nil {
		return aiagent.Plan{}, nil, fmt.Errorf("planner: %w", err)
	}

	var plan aiagent.Plan
	if err := json.Unmarshal(resp.Content, &plan); err != nil {
		return aiagent.Plan{}, nil, fmt.Errorf("decode plan: %w (raw: %s)", err, truncateStr(resp.RawText, 200))
	}
	return plan, resp, nil
}

// buildContextPayload gathers memory, account, contracts, market snapshots.
func buildContextPayload(ctx context.Context, a *app.App, prompt, trigger string) (json.RawMessage, error) {
	// Memory.
	memBody := ""
	if a.Cfg.Memory.Dir != "" {
		s, err := memory.Open(a.Cfg.Memory.Dir)
		if err == nil {
			body, err := s.ReadAll()
			if err == nil {
				memBody = body
			}
		}
	}

	// Contracts.
	var contractsRaw json.RawMessage
	info, err := a.Client.GetExchangeInfo(ctx)
	if err == nil && info != nil {
		b, merr := json.Marshal(info.FutureContracts)
		if merr == nil {
			contractsRaw = b
		}
	}

	// Account.
	account := map[string]any{}
	if ai, err := a.Client.GetAccountInfo(ctx); err == nil && ai != nil {
		account["info"] = ai
	}
	if ords, err := a.Client.GetPendingOrders(ctx); err == nil {
		account["pending_orders"] = ords
	}
	if a.Cfg.API.APIKey != "" && a.Cfg.API.AccountID != 0 {
		snapCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if snap, err := fetchAccountSnapshot(snapCtx, a); err == nil && snap != nil {
			account["positions"] = snap.Positions
			account["balance"] = snap.Balance
		}
		cancel()
	}
	var accountRaw json.RawMessage
	if len(account) > 0 {
		b, merr := json.Marshal(account)
		if merr == nil {
			accountRaw = b
		}
	}

	// Market: detect tokens in prompt.
	syms := symbolsFromPrompt(prompt, info)
	market := map[string]any{}
	for _, sym := range syms {
		if pr, err := a.Client.GetPrices(ctx, sym); err == nil && pr != nil {
			market[sym] = pr
		}
	}
	var marketRaw json.RawMessage
	if len(market) > 0 {
		b, merr := json.Marshal(market)
		if merr == nil {
			marketRaw = b
		}
	}

	return aiagent.BuildPayload(aiagent.ContextInput{
		Trigger:    trigger,
		UserPrompt: prompt,
		Memory:     memBody,
		Account:    accountRaw,
		Market:     marketRaw,
		Contracts:  contractsRaw,
	})
}

// symbolsFromPrompt returns contract symbols whose base/quote token or display
// name appears in the prompt. Falls back to a small hardcoded list when
// contract info is missing.
func symbolsFromPrompt(prompt string, info *hibachi.ExchangeInfo) []string {
	upper := strings.ToUpper(prompt)
	seen := map[string]struct{}{}
	var out []string

	if info != nil {
		for _, c := range info.FutureContracts {
			needle := strings.ToUpper(c.UnderlyingSymbol)
			if needle == "" {
				continue
			}
			if containsWord(upper, needle) {
				if _, ok := seen[c.Symbol]; !ok {
					seen[c.Symbol] = struct{}{}
					out = append(out, c.Symbol)
				}
			}
		}
	}
	return out
}

func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	idx := 0
	for {
		p := strings.Index(haystack[idx:], needle)
		if p < 0 {
			return false
		}
		p += idx
		before := byte(' ')
		if p > 0 {
			before = haystack[p-1]
		}
		after := byte(' ')
		if p+len(needle) < len(haystack) {
			after = haystack[p+len(needle)]
		}
		if !isWordChar(before) && !isWordChar(after) {
			return true
		}
		idx = p + 1
		if idx >= len(haystack) {
			return false
		}
	}
}

func isWordChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// finishPlan renders the preview, applies safety gates, executes, persists.
func finishPlan(ctx context.Context, a *app.App, plan aiagent.Plan, resp *aiagent.Response) error {
	if plan.Reasoning != "" {
		fmt.Println(dim("reasoning: ") + plan.Reasoning)
	}
	renderPlanPreview(plan)

	readOnly := isReadOnlyPlan(plan)
	if readOnly {
		// Apply memory writes and delete, then return.
		if err := applyMemoryChanges(a, plan); err != nil {
			return err
		}
		return nil
	}

	if a.DryRun {
		fmt.Println(dim("[dry-run] not submitting"))
		if err := applyMemoryChanges(a, plan); err != nil {
			return err
		}
		return nil
	}

	skip := a.Yes || !a.Cfg.Safety.RequireConfirm
	ok, err := safety.Confirm(os.Stdout, os.Stdin, "About to execute the plan above.", skip)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("aborted")
		return nil
	}

	if err := a.EnsureSigner(); err != nil {
		return fmt.Errorf("signer: %w", err)
	}

	j, err := journal.Open(a.Cfg.Journal.Path)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer j.Close()

	planJSON, _ := json.Marshal(plan)
	planEventID, err := j.Record(ctx, journal.Event{
		Kind:    "ai_plan",
		Agent:   fmt.Sprintf("%s:%s", planner_safe(a), model_safe(a)),
		Payload: planJSON,
	})
	if err != nil {
		return fmt.Errorf("journal record plan: %w", err)
	}
	_ = planEventID

	limits := safetyLimits(a)
	results := aiagent.Execute(ctx, aiagent.ExecutorDeps{
		Client: a.Client,
		Signer: a.Signer,
		SafetyCheck: func(kind, symbol string, notionalUSD float64) error {
			return limits.Check(safety.Action{Kind: kind, Symbol: symbol, NotionalUSD: notionalUSD})
		},
		OnAction: func(act aiagent.Action, r aiagent.ActionResult) {
			actJSON, _ := json.Marshal(struct {
				Action aiagent.Action       `json:"action"`
				Result aiagent.ActionResult `json:"result"`
			}{act, r})
			_, _ = j.Record(ctx, journal.Event{
				Kind:    "ai_action",
				Symbol:  act.Symbol,
				Agent:   fmt.Sprintf("%s:%s", planner_safe(a), model_safe(a)),
				Payload: actJSON,
			})
		},
	}, plan)

	if err := applyMemoryChanges(a, plan); err != nil {
		fmt.Fprintf(os.Stderr, "warn: memory: %v\n", err)
	}
	softWarnMemory(a)

	if resp != nil && resp.CostUSD > 0 {
		fmt.Printf("%s tokens_in=%d tokens_out=%d cost=$%.4f\n",
			dim("usage:"), resp.TokensIn, resp.TokensOut, resp.CostUSD)
	}
	renderResults(results)
	return nil
}

func planner_safe(a *app.App) string {
	return a.Cfg.AI.Backend
}

func model_safe(a *app.App) string {
	switch a.Cfg.AI.Backend {
	case "openrouter":
		return a.Cfg.AI.OpenRouter.Model
	default:
		return a.Cfg.AI.ClaudeCode.Model
	}
}

// applyMemoryChanges writes/deletes memory per the plan.
func applyMemoryChanges(a *app.App, plan aiagent.Plan) error {
	if len(plan.MemoryWrites) == 0 && len(plan.MemoryDeletes) == 0 {
		return nil
	}
	s, err := memory.Open(a.Cfg.Memory.Dir)
	if err != nil {
		return err
	}
	for _, w := range plan.MemoryWrites {
		if err := s.Write(w.File, w.Content); err != nil {
			return fmt.Errorf("memory write %s: %w", w.File, err)
		}
	}
	for _, name := range plan.MemoryDeletes {
		if err := s.Delete(name); err != nil {
			// Non-fatal: file may already be gone.
			fmt.Fprintf(os.Stderr, "warn: memory delete %s: %v\n", name, err)
		}
	}
	return nil
}

func softWarnMemory(a *app.App) {
	if a.Cfg.Memory.SoftTokens <= 0 {
		return
	}
	s, err := memory.Open(a.Cfg.Memory.Dir)
	if err != nil {
		return
	}
	tokens, err := s.EstimateTokens()
	if err != nil {
		return
	}
	if tokens > a.Cfg.Memory.SoftTokens {
		fmt.Fprintf(os.Stderr, "warn: memory ~%d tokens exceeds soft cap %d\n", tokens, a.Cfg.Memory.SoftTokens)
	}
}

// renderPlanPreview prints a table of actions plus memory changes.
func renderPlanPreview(plan aiagent.Plan) {
	if len(plan.Actions) > 0 {
		headers := []string{"Kind", "Symbol", "Side", "Qty", "Price", "OrderID", "Reason"}
		rows := make([][]string, 0, len(plan.Actions))
		for _, act := range plan.Actions {
			oid := ""
			if act.OrderID != nil {
				oid = strconv.FormatInt(*act.OrderID, 10)
			}
			rows = append(rows, []string{
				act.Kind, act.Symbol, act.Side, act.Qty, act.Price, oid, act.Reason,
			})
		}
		output.PrintTable(headers, rows, output.NumericAligns(headers, "Qty", "Price", "OrderID"))
	} else {
		fmt.Println(dim("(no actions)"))
	}

	if len(plan.MemoryWrites) > 0 || len(plan.MemoryDeletes) > 0 {
		fmt.Println(dim("memory changes:"))
		for _, w := range plan.MemoryWrites {
			fmt.Printf("  write %s (%d bytes)\n", w.File, len(w.Content))
		}
		for _, name := range plan.MemoryDeletes {
			fmt.Printf("  delete %s\n", name)
		}
	}
	if plan.Ask != "" {
		fmt.Println(color.New(color.FgYellow).Sprint("ask: ") + plan.Ask)
	}
}

func renderResults(results []aiagent.ActionResult) {
	if len(results) == 0 {
		return
	}
	headers := []string{"Kind", "Symbol", "OK", "OrderID", "Message"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		ok := "no"
		if r.OK {
			ok = "yes"
		}
		msg := r.Message
		if r.Err != nil {
			msg = r.Err.Error()
		}
		oid := ""
		if r.OrderID != 0 {
			oid = strconv.FormatInt(r.OrderID, 10)
		}
		rows = append(rows, []string{r.Action.Kind, r.Action.Symbol, ok, oid, msg})
	}
	output.PrintTable(headers, rows, output.NumericAligns(headers, "OrderID"))
}

// isReadOnlyPlan returns true if the plan has no trade-affecting actions.
func isReadOnlyPlan(plan aiagent.Plan) bool {
	if len(plan.Actions) == 0 {
		return true
	}
	for _, a := range plan.Actions {
		switch a.Kind {
		case aiagent.ActionGetContext, aiagent.ActionDone, "":
			continue
		default:
			return false
		}
	}
	return true
}

// pickSession picks a registry entry to resume, or creates a fresh one.
// Backend mismatch or idle timeout discards the stored session.
func pickSession(reg map[string]*sessionEntry, name string, planner aiagent.Planner, forceFresh bool) *sessionEntry {
	existing := reg[name]
	now := time.Now().UTC()
	if forceFresh || existing == nil || existing.Backend != planner.Backend() ||
		now.Sub(existing.LastUsed) > sessionIdleExpiry {
		return &sessionEntry{Name: name, Backend: planner.Backend(), Model: planner.Model(), CreatedAt: now}
	}
	return existing
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func dim(s string) string {
	if !output.IsTTY() {
		return s
	}
	return color.New(color.Faint).Sprint(s)
}

// ---------------- chat REPL ----------------

func newAIChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Interactive REPL",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAIChat(cmd)
		},
	}
}

func runAIChat(cmd *cobra.Command) error {
	a := app.From(cmd.Context())
	if err := a.EnsureClient(); err != nil {
		return err
	}
	planner, err := backend.New(a.Cfg)
	if err != nil {
		return err
	}
	defer planner.Close()

	regPath := sessionsPath(a.Cfg.Journal.Path)
	reg, err := loadSessions(regPath)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("chat:%d", time.Now().Unix())
	entry := &sessionEntry{Name: name, Backend: planner.Backend(), Model: planner.Model(), CreatedAt: time.Now().UTC()}
	reg[name] = entry
	_ = saveSessions(regPath, reg)

	fmt.Printf("hibachi ai chat (session %s, %s %s). type 'exit' or Ctrl+D to leave.\n",
		name, planner.Backend(), planner.Model())

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	ctx := cmd.Context()
	first := true
	for {
		fmt.Print("> ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}
		freshCall := first
		first = false

		plan, resp, err := planOnce(ctx, a, planner, line, "user-prompt", entry.SessionID, freshCall)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		entry.SessionID = resp.SessionID
		entry.LastUsed = time.Now().UTC()
		entry.Turns += 1 + resp.NumTurns
		entry.CostUSD += resp.CostUSD
		reg[name] = entry
		_ = saveSessions(regPath, reg)

		if err := finishPlan(ctx, a, plan, resp); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Println("bye")
	return nil
}

// ---------------- backend / models ----------------

func newAIBackendCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backend",
		Short: "Show or change the active backend",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			count := 0
			if reg, err := loadSessions(sessionsPath(a.Cfg.Journal.Path)); err == nil {
				count = len(reg)
			}
			pairs := [][2]string{
				{"backend", a.Cfg.AI.Backend},
				{"model", model_safe(a)},
				{"sessions", strconv.Itoa(count)},
			}
			output.PrintKV(pairs)
			return nil
		},
	}
	c.AddCommand(newAIBackendUseCmd())
	return c
}

func newAIBackendUseCmd() *cobra.Command {
	var modelFlag string
	c := &cobra.Command{
		Use:   "use [name]",
		Short: "Switch backend (claude-code | openrouter)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			name := strings.TrimSpace(args[0])
			switch name {
			case "claude-code", "openrouter":
				// OK.
			default:
				return fmt.Errorf("unknown backend %q (valid: claude-code, openrouter)", name)
			}
			a.Cfg.AI.Backend = name
			if modelFlag != "" {
				switch name {
				case "claude-code":
					a.Cfg.AI.ClaudeCode.Model = modelFlag
				case "openrouter":
					a.Cfg.AI.OpenRouter.Model = modelFlag
				}
			}
			if err := config.Save(resolveConfigPath(), a.Cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("backend set to %s (model=%s)\n", name, model_safe(a))
			return nil
		},
	}
	c.Flags().StringVar(&modelFlag, "model", "", "set model for this backend")
	return c
}

func newAIModelsCmd() *cobra.Command {
	c := &cobra.Command{Use: "models", Short: "List / select models"}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List preset models",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			current := fmt.Sprintf("%s:%s", a.Cfg.AI.Backend, model_safe(a))
			for _, m := range modelPresets {
				prefix := "  "
				if m == current {
					if output.IsTTY() {
						prefix = color.New(color.FgGreen, color.Bold).Sprint("* ")
					} else {
						prefix = "* "
					}
				}
				fmt.Printf("%s%s\n", prefix, m)
			}
			return nil
		},
	})
	return c
}

// ---------------- keys ----------------

func newAIKeysCmd() *cobra.Command {
	c := &cobra.Command{Use: "keys", Short: "Manage backend API keys"}
	c.AddCommand(&cobra.Command{
		Use:   "set [backend]",
		Short: "Store an API key in the OS keychain",
		Args:  cobra.ExactArgs(1),
		RunE:  runAIKeysSet,
	})
	return c
}

func runAIKeysSet(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	var user string
	switch name {
	case "openrouter":
		user = "openrouter-api-key"
	default:
		return fmt.Errorf("unsupported backend %q for keys set", name)
	}
	fmt.Printf("API key for %s (input hidden): ", name)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return errors.New("empty api key")
	}
	if err := keyring.Set(keyringServiceName, user, key); err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	fmt.Printf("stored %s api key in keychain (service=%s, user=%s)\n", name, keyringServiceName, user)
	return nil
}

// ---------------- usage ----------------

func newAIUsageCmd() *cobra.Command {
	var today bool
	var since string
	c := &cobra.Command{
		Use:   "usage",
		Short: "Show AI token and cost usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			reg, err := loadSessions(sessionsPath(a.Cfg.Journal.Path))
			if err != nil {
				return err
			}
			cutoff := time.Time{}
			switch {
			case today:
				now := time.Now().UTC()
				cutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			case since != "":
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("parse --since: %w", err)
				}
				cutoff = time.Now().UTC().Add(-d)
			}
			// Roll up by backend:model.
			type agg struct {
				turns    int
				cost     float64
				sessions int
			}
			roll := map[string]*agg{}
			for _, s := range reg {
				if !cutoff.IsZero() && s.LastUsed.Before(cutoff) {
					continue
				}
				k := s.Backend + ":" + s.Model
				a := roll[k]
				if a == nil {
					a = &agg{}
					roll[k] = a
				}
				a.turns += s.Turns
				a.cost += s.CostUSD
				a.sessions++
			}
			keys := make([]string, 0, len(roll))
			for k := range roll {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			headers := []string{"Backend:Model", "Sessions", "Turns", "CostUSD"}
			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				v := roll[k]
				rows = append(rows, []string{k, strconv.Itoa(v.sessions), strconv.Itoa(v.turns), fmt.Sprintf("%.4f", v.cost)})
			}
			output.PrintTable(headers, rows, output.NumericAligns(headers, "Sessions", "Turns", "CostUSD"))
			return nil
		},
	}
	c.Flags().BoolVar(&today, "today", false, "only today (UTC)")
	c.Flags().StringVar(&since, "since", "", "rolling window, e.g. 24h, 7d-style duration accepted by time.ParseDuration")
	return c
}

// ---------------- sessions ----------------

func newAISessionsCmd() *cobra.Command {
	c := &cobra.Command{Use: "sessions", Short: "Inspect stored AI sessions"}
	c.AddCommand(
		&cobra.Command{Use: "list", Short: "List sessions", RunE: runAISessionsList},
		&cobra.Command{Use: "show [name]", Short: "Show session transcript", Args: cobra.ExactArgs(1), RunE: runAISessionsShow},
		&cobra.Command{Use: "clear [name]", Short: "Delete a session", Args: cobra.ExactArgs(1), RunE: runAISessionsClear},
	)
	return c
}

func runAISessionsList(cmd *cobra.Command, _ []string) error {
	a := app.From(cmd.Context())
	reg, err := loadSessions(sessionsPath(a.Cfg.Journal.Path))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(reg))
	for k := range reg {
		names = append(names, k)
	}
	sort.Strings(names)
	headers := []string{"Name", "Backend", "Model", "LastUsed", "Turns", "CostUSD"}
	rows := make([][]string, 0, len(names))
	for _, n := range names {
		s := reg[n]
		rows = append(rows, []string{
			n, s.Backend, s.Model, s.LastUsed.Format(time.RFC3339),
			strconv.Itoa(s.Turns), fmt.Sprintf("%.4f", s.CostUSD),
		})
	}
	output.PrintTable(headers, rows, output.NumericAligns(headers, "Turns", "CostUSD"))
	return nil
}

func runAISessionsShow(cmd *cobra.Command, args []string) error {
	a := app.From(cmd.Context())
	reg, err := loadSessions(sessionsPath(a.Cfg.Journal.Path))
	if err != nil {
		return err
	}
	name := args[0]
	s := reg[name]
	if s == nil {
		return fmt.Errorf("no session named %q", name)
	}
	switch s.Backend {
	case "openrouter":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path := filepath.Join(home, ".hibachi", "sessions", s.SessionID+".jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read transcript: %w", err)
		}
		_, _ = os.Stdout.Write(data)
		return nil
	default:
		fmt.Printf("transcript for %s backend is managed by the backend itself; use the backend's own logs.\n", s.Backend)
		fmt.Printf("session_id: %s\n", s.SessionID)
		return nil
	}
}

func runAISessionsClear(cmd *cobra.Command, args []string) error {
	a := app.From(cmd.Context())
	regPath := sessionsPath(a.Cfg.Journal.Path)
	reg, err := loadSessions(regPath)
	if err != nil {
		return err
	}
	name := args[0]
	s := reg[name]
	if s == nil {
		return fmt.Errorf("no session named %q", name)
	}
	home, err := os.UserHomeDir()
	if err == nil && s.SessionID != "" {
		_ = os.Remove(filepath.Join(home, ".hibachi", "sessions", s.SessionID+".jsonl"))
	}
	delete(reg, name)
	if err := saveSessions(regPath, reg); err != nil {
		return err
	}
	fmt.Printf("cleared %s\n", name)
	return nil
}
