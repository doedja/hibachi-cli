package dash

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/doedja/hibachi-go/ws"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/memory"
	"github.com/doedja/hibachi-cli/internal/safety"
)

// tickCmd emits a TickMsg every second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg{T: t.UTC()}
	})
}

// loadContractsCmd fetches the exchange info once at startup.
func loadContractsCmd(deps Deps) tea.Cmd {
	return func() tea.Msg {
		if deps.Client == nil {
			return nil
		}
		info, err := deps.Client.GetExchangeInfo(context.Background())
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return contractsLoadedMsg{info: info}
	}
}

// contractsLoadedMsg carries the exchange info into the model for lookups.
type contractsLoadedMsg struct {
	info *hibachi.ExchangeInfo
}

// planRunCmd dispatches the planner asynchronously.
func planRunCmd(deps Deps, prompt string) tea.Cmd {
	return func() tea.Msg {
		if deps.Planner == nil {
			return PlanMsg{Prompt: prompt, Err: errNoPlanner}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		payload, err := buildPlannerPayload(ctx, deps, prompt)
		if err != nil {
			return PlanMsg{Prompt: prompt, Err: err}
		}
		sys, err := aiagent.SystemPrompt("oneshot")
		if err != nil {
			return PlanMsg{Prompt: prompt, Err: err}
		}
		resp, err := deps.Planner.Plan(ctx, aiagent.Request{
			SystemPrompt: sys,
			UserPayload:  payload,
			Fresh:        true,
		})
		if err != nil {
			return PlanMsg{Prompt: prompt, Err: err}
		}
		var plan aiagent.Plan
		if err := json.Unmarshal(resp.Content, &plan); err != nil {
			return PlanMsg{Prompt: prompt, Err: err}
		}
		return PlanMsg{Plan: plan, Resp: resp, Prompt: prompt}
	}
}

// executePlanCmd runs the plan through the executor.
func executePlanCmd(deps Deps, plan aiagent.Plan) tea.Cmd {
	return func() tea.Msg {
		if deps.App != nil {
			if err := deps.App.EnsureSigner(); err != nil {
				return ExecuteResultMsg{Err: err}
			}
			deps.Signer = deps.App.Signer
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		limits := safety.Limits{
			MaxNotionalUSD: deps.Cfg.Safety.MaxNotionalUSD,
			Symbols:        deps.Cfg.Safety.Symbols,
			RequireConfirm: deps.Cfg.Safety.RequireConfirm,
			DryRun:         deps.App != nil && deps.App.DryRun,
		}
		results := aiagent.Execute(ctx, aiagent.ExecutorDeps{
			Client: deps.Client,
			Signer: deps.Signer,
			SafetyCheck: func(kind, symbol string, notionalUSD float64) error {
				return limits.Check(safety.Action{Kind: kind, Symbol: symbol, NotionalUSD: notionalUSD})
			},
		}, plan)
		return ExecuteResultMsg{Results: results}
	}
}

// buildPlannerPayload mirrors cmd/ai.go's buildContextPayload but tuned for the dash.
func buildPlannerPayload(ctx context.Context, deps Deps, prompt string) (json.RawMessage, error) {
	memBody := ""
	if deps.Memory != nil {
		if body, err := deps.Memory.ReadAll(); err == nil {
			memBody = body
		}
	} else if deps.Cfg != nil && deps.Cfg.Memory.Dir != "" {
		if s, err := memory.Open(deps.Cfg.Memory.Dir); err == nil {
			if body, err := s.ReadAll(); err == nil {
				memBody = body
			}
		}
	}

	var contractsRaw json.RawMessage
	if info, err := deps.Client.GetExchangeInfo(ctx); err == nil && info != nil {
		if b, err := json.Marshal(info.FutureContracts); err == nil {
			contractsRaw = b
		}
	}

	account := map[string]any{}
	if ai, err := deps.Client.GetAccountInfo(ctx); err == nil && ai != nil {
		account["info"] = ai
	}
	if ords, err := deps.Client.GetPendingOrders(ctx); err == nil {
		account["pending_orders"] = ords
	}
	var accountRaw json.RawMessage
	if len(account) > 0 {
		if b, err := json.Marshal(account); err == nil {
			accountRaw = b
		}
	}

	market := map[string]any{}
	if deps.InitialSym != "" {
		if pr, err := deps.Client.GetPrices(ctx, deps.InitialSym); err == nil && pr != nil {
			market[deps.InitialSym] = pr
		}
	}
	var marketRaw json.RawMessage
	if len(market) > 0 {
		if b, err := json.Marshal(market); err == nil {
			marketRaw = b
		}
	}

	return aiagent.BuildPayload(aiagent.ContextInput{
		Trigger:    "dash-prompt",
		UserPrompt: prompt,
		Memory:     memBody,
		Account:    accountRaw,
		Market:     marketRaw,
		Contracts:  contractsRaw,
	})
}

// startFeeds launches the WS + advisor-tail goroutines.
func startFeeds(ctx context.Context, deps Deps, send func(tea.Msg)) {
	// Focused symbol at feed-start time. Future focus changes require
	// restart of the market feed; v0.1 simplification is documented upstream.
	if deps.Client != nil {
		go initialRESTPump(ctx, deps, send)
		go marketFeed(ctx, deps, send)
		go watchlistFeed(ctx, deps, send)
	}
	if deps.Cfg != nil && deps.Cfg.API.APIKey != "" && deps.Cfg.API.AccountID != 0 {
		go accountFeed(ctx, deps, send)
	}
	if deps.Journal != nil {
		go advisorTail(ctx, deps, send)
	}
}

func initialRESTPump(ctx context.Context, deps Deps, send func(tea.Msg)) {
	if ai, err := deps.Client.GetAccountInfo(ctx); err == nil {
		send(AccountInfoMsg{Info: ai})
	}
	if orders, err := deps.Client.GetPendingOrders(ctx); err == nil {
		send(PendingOrdersMsg{Orders: orders})
	}
	if deps.InitialSym != "" {
		if ob, err := deps.Client.GetOrderbook(ctx, deps.InitialSym, 12, 0); err == nil {
			send(OrderbookMsg{Symbol: deps.InitialSym, Book: ob})
		}
	}

	// Periodic REST refresh every 15 seconds (covers REST-only account info).
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if ai, err := deps.Client.GetAccountInfo(ctx); err == nil {
				send(AccountInfoMsg{Info: ai})
			}
			if orders, err := deps.Client.GetPendingOrders(ctx); err == nil {
				send(PendingOrdersMsg{Orders: orders})
			}
		}
	}
}

func marketFeed(ctx context.Context, deps Deps, send func(tea.Msg)) {
	if deps.InitialSym == "" {
		send(MarketStatusMsg{Status: ConnError, Reason: "no focused symbol", UpdatedAt: time.Now()})
		return
	}
	send(MarketStatusMsg{Status: ConnConnecting, UpdatedAt: time.Now()})

	client := ws.NewMarketClient(ws.MarketClientOptions{})
	client.OnReconnect(func() {
		send(MarketStatusMsg{Status: ConnLive, Reason: "reconnected", UpdatedAt: time.Now()})
	})
	client.OnDisconnect(func(err error) {
		send(MarketStatusMsg{Status: ConnReconnecting, Reason: safeErr(err), UpdatedAt: time.Now()})
	})

	sym := deps.InitialSym
	client.On("orderbook", func(data json.RawMessage) {
		var book hibachi.OrderBook
		if err := json.Unmarshal(data, &book); err == nil {
			send(OrderbookMsg{Symbol: sym, Book: &book})
		}
	})
	client.On("trades", func(data json.RawMessage) {
		var tr struct {
			Trades []struct {
				Price     string `json:"price"`
				Quantity  string `json:"quantity"`
				Timestamp int64  `json:"timestamp"`
				TakerSide string `json:"takerSide"`
			} `json:"trades"`
		}
		if err := json.Unmarshal(data, &tr); err == nil {
			for _, t := range tr.Trades {
				send(TradeMsg{Symbol: sym, Price: t.Price, Quantity: t.Quantity, Timestamp: t.Timestamp, Taker: t.TakerSide})
			}
			return
		}
		// Some servers send one trade per event rather than a batch.
		var single struct {
			Price     string `json:"price"`
			Quantity  string `json:"quantity"`
			Timestamp int64  `json:"timestamp"`
			TakerSide string `json:"takerSide"`
		}
		if err := json.Unmarshal(data, &single); err == nil {
			send(TradeMsg{Symbol: sym, Price: single.Price, Quantity: single.Quantity, Timestamp: single.Timestamp, Taker: single.TakerSide})
		}
	})
	client.On("mark_price", func(data json.RawMessage) {
		var p struct {
			Price string `json:"price"`
		}
		if err := json.Unmarshal(data, &p); err == nil {
			send(PriceTickMsg{Symbol: sym, Mark: p.Price})
		}
	})

	if err := client.Connect(ctx); err != nil {
		send(MarketStatusMsg{Status: ConnError, Reason: safeErr(err), UpdatedAt: time.Now()})
		return
	}
	defer client.Disconnect()

	depth := 12
	if err := client.Subscribe(ctx,
		hibachi.WSSubscription{Topic: hibachi.WSTopicOrderbook, Symbol: sym, Depth: &depth},
		hibachi.WSSubscription{Topic: hibachi.WSTopicTrades, Symbol: sym},
		hibachi.WSSubscription{Topic: hibachi.WSTopicMarkPrice, Symbol: sym},
	); err != nil {
		send(MarketStatusMsg{Status: ConnError, Reason: safeErr(err), UpdatedAt: time.Now()})
		return
	}
	send(MarketStatusMsg{Status: ConnLive, UpdatedAt: time.Now()})

	select {
	case <-ctx.Done():
	case err := <-client.Done():
		if err != nil {
			send(MarketStatusMsg{Status: ConnError, Reason: safeErr(err), UpdatedAt: time.Now()})
		}
	}
}

func watchlistFeed(ctx context.Context, deps Deps, send func(tea.Msg)) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	pump := func() {
		for _, sym := range DefaultWatchlist() {
			if pr, err := deps.Client.GetPrices(ctx, sym); err == nil && pr != nil {
				send(PriceTickMsg{Symbol: sym, Mark: pr.MarkPrice, Ask: pr.AskPrice, Bid: pr.BidPrice})
			}
		}
	}
	pump()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			pump()
		}
	}
}

func accountFeed(ctx context.Context, deps Deps, send func(tea.Msg)) {
	send(AccountStatusMsg{Status: ConnConnecting, UpdatedAt: time.Now()})
	client := ws.NewAccountClient(ws.AccountClientOptions{
		APIKey:    deps.Cfg.API.APIKey,
		AccountID: deps.Cfg.API.AccountID,
	})
	client.OnReconnect(func(res *hibachi.AccountStreamStartResult) {
		if res != nil {
			send(AccountSnapshotMsg{Snapshot: &res.AccountSnapshot})
		}
		send(AccountStatusMsg{Status: ConnLive, Reason: "reconnected", UpdatedAt: time.Now()})
	})
	client.OnDisconnect(func(err error) {
		send(AccountStatusMsg{Status: ConnReconnecting, Reason: safeErr(err), UpdatedAt: time.Now()})
	})
	client.OnAll(func(topic string, data json.RawMessage) {
		send(AccountEventMsg{Topic: topic, Data: data})
	})

	if err := client.Connect(ctx); err != nil {
		send(AccountStatusMsg{Status: ConnError, Reason: safeErr(err), UpdatedAt: time.Now()})
		return
	}
	defer client.Disconnect()

	res, err := client.StreamStart(ctx)
	if err != nil {
		send(AccountStatusMsg{Status: ConnError, Reason: safeErr(err), UpdatedAt: time.Now()})
		return
	}
	send(AccountSnapshotMsg{Snapshot: &res.AccountSnapshot})
	send(AccountStatusMsg{Status: ConnLive, UpdatedAt: time.Now()})

	if err := client.ListenLoop(ctx); err != nil && ctx.Err() == nil {
		send(AccountStatusMsg{Status: ConnError, Reason: safeErr(err), UpdatedAt: time.Now()})
	}
}

func advisorTail(ctx context.Context, deps Deps, send func(tea.Msg)) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	var lastID int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			evs, err := deps.Journal.Recent(ctx, 20)
			if err != nil {
				continue
			}
			// Recent returns DESC; find newest advisor_tick.
			for _, e := range evs {
				if e.Kind == "advisor_tick" && e.ID > lastID {
					lastID = e.ID
					body, reason := extractAdvisor(e.Payload)
					send(AdvisorTickMsg{
						At:      e.Timestamp,
						Session: e.Agent,
						Body:    body,
						Reason:  reason,
						Raw:     e.Payload,
					})
					break
				}
			}
		}
	}
}

func extractAdvisor(payload json.RawMessage) (body, reason string) {
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	if v, ok := m["body"].(string); ok {
		body = v
	} else if v, ok := m["suggestion"].(string); ok {
		body = v
	}
	if v, ok := m["reason"].(string); ok {
		reason = v
	} else if v, ok := m["reasoning"].(string); ok {
		reason = v
	}
	body = strings.TrimSpace(body)
	reason = strings.TrimSpace(reason)
	return body, reason
}

func safeErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// errNoPlanner is a sentinel returned when the AI backend is unconfigured.
type errNoPlannerT string

func (e errNoPlannerT) Error() string { return string(e) }

var errNoPlanner = errNoPlannerT("ai planner not configured; run `hibachi ai backend use ...` first")
