package dash

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shopspring/decimal"

	hibachi "github.com/doedja/hibachi-go"
)

func parseDec(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(strings.TrimSpace(s))
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TickMsg:
		m.now = msg.T
		if !m.bannerUntil.IsZero() && m.now.After(m.bannerUntil) {
			m.banner = ""
			m.bannerUntil = time.Time{}
		}
		return m, tickCmd()

	case contractsLoadedMsg:
		if msg.info != nil {
			for _, c := range msg.info.FutureContracts {
				m.contractsBySymbol[c.Symbol] = c
			}
		}
		return m, nil

	case OrderbookMsg:
		if msg.Symbol == m.focusedSymbol {
			m.orderbook = msg.Book
			m.lastOBAt = time.Now().UTC()
		}
		return m, nil

	case TradeMsg:
		m.pushTrade(msg)
		if msg.Symbol != "" {
			m.pushWatchPoint(msg.Symbol, msg.Price)
		}
		return m, nil

	case PriceTickMsg:
		if msg.Symbol != "" {
			cur := m.watchPrices[msg.Symbol]
			if msg.Mark != "" {
				cur.Mark = msg.Mark
			}
			if msg.Ask != "" {
				cur.Ask = msg.Ask
			}
			if msg.Bid != "" {
				cur.Bid = msg.Bid
			}
			cur.Symbol = msg.Symbol
			m.watchPrices[msg.Symbol] = cur
			if msg.Mark != "" {
				m.pushWatchPoint(msg.Symbol, msg.Mark)
			}
		}
		return m, nil

	case AccountInfoMsg:
		m.accountInfo = msg.Info
		return m, nil

	case AccountSnapshotMsg:
		m.snapshot = msg.Snapshot
		if m.positionIdx >= len(m.positions()) {
			m.positionIdx = 0
		}
		return m, nil

	case AccountEventMsg:
		// Merge account stream updates into pending orders / positions.
		m.applyAccountEvent(msg)
		return m, nil

	case PendingOrdersMsg:
		m.pendingOrders = msg.Orders
		if m.orderIdx >= len(m.pendingOrders) {
			m.orderIdx = 0
		}
		return m, nil

	case MarketStatusMsg:
		m.marketStatus = msg.Status
		m.statusReason = msg.Reason
		return m, nil

	case AccountStatusMsg:
		m.accountStatus = msg.Status
		if msg.Reason != "" {
			m.statusReason = msg.Reason
		}
		return m, nil

	case AdvisorTickMsg:
		m.advisor = &msg
		m.lastAdvisorTS = msg.At
		return m, nil

	case PlanMsg:
		m.overlay = OverlayPlan
		if msg.Err != nil {
			m.overlay = OverlayError
			m.lastError = msg.Err.Error()
			return m, nil
		}
		plan := msg.Plan
		m.lastPlan = &plan
		m.lastResp = msg.Resp
		m.lastPrompt = msg.Prompt
		return m, nil

	case ExecuteResultMsg:
		m.overlay = OverlayNone
		if msg.Err != nil {
			m.lastError = msg.Err.Error()
			return m, flashBanner("execute: " + msg.Err.Error())
		}
		ok, fail := 0, 0
		for _, r := range msg.Results {
			if r.OK {
				ok++
			} else {
				fail++
			}
		}
		return m, flashBanner(fmt.Sprintf("executed: %d ok, %d failed", ok, fail))

	case BannerMsg:
		m.banner = msg.Text
		ttl := msg.TTL
		if ttl == 0 {
			ttl = 3 * time.Second
		}
		m.bannerUntil = time.Now().Add(ttl)
		return m, nil

	case ErrorMsg:
		if msg.Err != nil {
			m.lastError = msg.Err.Error()
			return m, flashBanner("error: " + msg.Err.Error())
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay-first routing.
	switch m.overlay {
	case OverlayPrompt:
		return m.handlePromptKey(msg)
	case OverlayPlan:
		return m.handlePlanKey(msg)
	case OverlayConfirmClose:
		return m.handleConfirmCloseKey(msg)
	case OverlayConfirmCancel:
		return m.handleConfirmCancelKey(msg)
	case OverlayHelp, OverlayError, OverlayPlanning:
		if key.Matches(msg, m.keys.Escape) || key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Enter) {
			m.overlay = OverlayNone
			m.lastError = ""
		}
		return m, nil
	}

	// Main key routes.
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Prompt):
		if m.deps.Planner == nil {
			return m, flashBanner("ai backend not configured")
		}
		m.overlay = OverlayPrompt
		m.prompt = ""
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.overlay = OverlayHelp
		return m, nil
	case key.Matches(msg, m.keys.Tab):
		m.focus = (m.focus + 1) % 5
		return m, nil
	case key.Matches(msg, m.keys.ShiftTab):
		m.focus = (m.focus + 4) % 5
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.moveSelection(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.moveSelection(1)
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		return m, manualRefreshCmd(m.deps)
	case msg.String() == "1":
		return m.selectWatch(0), nil
	case msg.String() == "2":
		return m.selectWatch(1), nil
	case msg.String() == "3":
		return m.selectWatch(2), nil
	case msg.String() == "4":
		return m.selectWatch(3), nil
	case msg.String() == "5":
		return m.selectWatch(4), nil
	case msg.String() == "6":
		return m.selectWatch(5), nil
	case msg.String() == "7":
		return m.selectWatch(6), nil
	case msg.String() == "8":
		return m.selectWatch(7), nil
	case msg.String() == "9":
		return m.selectWatch(8), nil
	case msg.String() == "b":
		return m, flashBanner("[v0.2] quick-buy form pending")
	case msg.String() == "s":
		return m, flashBanner("[v0.2] quick-sell form pending")
	case msg.String() == "c":
		if m.focusedPosition() == nil {
			return m, flashBanner("no position selected")
		}
		m.overlay = OverlayConfirmClose
		return m, nil
	case msg.String() == "x":
		if m.focusedOrder() == nil {
			return m, flashBanner("no order selected")
		}
		m.overlay = OverlayConfirmCancel
		return m, nil
	case msg.String() == "a":
		if m.advisor != nil {
			// Apply latest advisor suggestion via planner route.
			return m.applyAdvisor()
		}
		m.advisorOn = !m.advisorOn
		return m, nil
	case msg.String() == "i":
		m.advisor = nil
		return m, flashBanner("advisor ignored")
	}
	return m, nil
}

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.overlay = OverlayNone
		m.prompt = ""
		return m, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.prompt)
		if text == "" {
			return m, nil
		}
		m.overlay = OverlayPlanning
		return m, planRunCmd(m.deps, text)
	case tea.KeyBackspace:
		if len(m.prompt) > 0 {
			m.prompt = m.prompt[:len(m.prompt)-1]
		}
		return m, nil
	case tea.KeyRunes:
		m.prompt += string(msg.Runes)
		return m, nil
	case tea.KeySpace:
		m.prompt += " "
		return m, nil
	}
	return m, nil
}

func (m Model) handlePlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.lastPlan == nil {
			m.overlay = OverlayNone
			return m, nil
		}
		if m.deps.App != nil && m.deps.App.DryRun {
			m.overlay = OverlayNone
			return m, flashBanner("[dry-run] plan not submitted")
		}
		plan := *m.lastPlan
		return m, executePlanCmd(m.deps, plan)
	case "n", "N", "esc":
		m.overlay = OverlayNone
		return m, flashBanner("plan dismissed")
	case "e", "E":
		// Edit: return to prompt with the old text pre-loaded.
		m.overlay = OverlayPrompt
		m.prompt = m.lastPrompt
		return m, nil
	case "d", "D":
		m.overlay = OverlayNone
		if m.lastPlan != nil {
			b, _ := json.MarshalIndent(m.lastPlan, "", "  ")
			return m, flashBanner(truncate(string(b), 120))
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleConfirmCloseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		pos := m.focusedPosition()
		m.overlay = OverlayNone
		if pos == nil {
			return m, flashBanner("no position")
		}
		return m, closePositionCmd(m.deps, *pos)
	case "n", "N", "esc":
		m.overlay = OverlayNone
	}
	return m, nil
}

func (m Model) handleConfirmCancelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		ord := m.focusedOrder()
		m.overlay = OverlayNone
		if ord == nil {
			return m, flashBanner("no order")
		}
		return m, cancelOrderCmd(m.deps, ord.OrderID)
	case "n", "N", "esc":
		m.overlay = OverlayNone
	}
	return m, nil
}

func (m *Model) pushTrade(t TradeMsg) {
	if m.maxTrades <= 0 {
		m.maxTrades = 40
	}
	rt := RecentTrade{Symbol: t.Symbol, Price: t.Price, Quantity: t.Quantity, Taker: t.Taker, Timestamp: t.Timestamp}
	m.recentTrades = append([]RecentTrade{rt}, m.recentTrades...)
	if len(m.recentTrades) > m.maxTrades {
		m.recentTrades = m.recentTrades[:m.maxTrades]
	}
}

func (m *Model) pushWatchPoint(sym, priceStr string) {
	if priceStr == "" {
		return
	}
	var price float64
	_, err := fmt.Sscanf(priceStr, "%f", &price)
	if err != nil {
		return
	}
	hist := m.watchHistory[sym]
	hist = append(hist, price)
	if len(hist) > 32 {
		hist = hist[len(hist)-32:]
	}
	m.watchHistory[sym] = hist
}

func (m *Model) moveSelection(delta int) {
	switch m.focus {
	case FocusPositions:
		n := len(m.positions())
		if n == 0 {
			return
		}
		m.positionIdx = clamp(m.positionIdx+delta, 0, n-1)
	case FocusOrders:
		n := len(m.pendingOrders)
		if n == 0 {
			return
		}
		m.orderIdx = clamp(m.orderIdx+delta, 0, n-1)
	case FocusWatchlist:
		n := len(m.watchlist)
		if n == 0 {
			return
		}
		m.watchIdx = clamp(m.watchIdx+delta, 0, n-1)
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m Model) selectWatch(idx int) Model {
	if idx < 0 || idx >= len(m.watchlist) {
		return m
	}
	m.watchIdx = idx
	newSym := m.watchlist[idx]
	if newSym != m.focusedSymbol {
		m.focusedSymbol = newSym
		// v0.1: market feed is tied to InitialSym; flash a hint until full restart lands.
		m.banner = "focused " + newSym + " (orderbook restart is v0.2)"
		m.bannerUntil = time.Now().Add(3 * time.Second)
	}
	return m
}

func (m Model) positions() []hibachi.Position {
	if m.snapshot == nil {
		return nil
	}
	return m.snapshot.Positions
}

func (m Model) focusedPosition() *hibachi.Position {
	ps := m.positions()
	if m.positionIdx < 0 || m.positionIdx >= len(ps) {
		return nil
	}
	p := ps[m.positionIdx]
	return &p
}

func (m Model) focusedOrder() *hibachi.Order {
	if m.orderIdx < 0 || m.orderIdx >= len(m.pendingOrders) {
		return nil
	}
	o := m.pendingOrders[m.orderIdx]
	return &o
}

func (m *Model) applyAccountEvent(e AccountEventMsg) {
	// Best-effort merge. Payload shapes vary. Unknown events are ignored.
	switch e.Topic {
	case "orders":
		var list []hibachi.Order
		if err := json.Unmarshal(e.Data, &list); err == nil {
			m.pendingOrders = filterPending(list)
			return
		}
	case "order":
		var one hibachi.Order
		if err := json.Unmarshal(e.Data, &one); err == nil {
			m.replaceOrder(one)
		}
	case "positions":
		var list []hibachi.Position
		if err := json.Unmarshal(e.Data, &list); err == nil {
			if m.snapshot == nil {
				m.snapshot = &hibachi.AccountSnapshot{}
			}
			m.snapshot.Positions = list
		}
	case "position":
		var p hibachi.Position
		if err := json.Unmarshal(e.Data, &p); err == nil {
			if m.snapshot == nil {
				m.snapshot = &hibachi.AccountSnapshot{}
			}
			m.snapshot.Positions = upsertPosition(m.snapshot.Positions, p)
		}
	case "balance":
		var bal struct {
			Balance string `json:"balance"`
		}
		if err := json.Unmarshal(e.Data, &bal); err == nil && bal.Balance != "" {
			if m.snapshot == nil {
				m.snapshot = &hibachi.AccountSnapshot{}
			}
			m.snapshot.Balance = bal.Balance
		}
	}
}

func filterPending(orders []hibachi.Order) []hibachi.Order {
	out := make([]hibachi.Order, 0, len(orders))
	for _, o := range orders {
		switch o.Status {
		case hibachi.OrderStatusPending, hibachi.OrderStatusPlaced, hibachi.OrderStatusPartiallyFilled, hibachi.OrderStatusChildPending:
			out = append(out, o)
		}
	}
	return out
}

func (m *Model) replaceOrder(o hibachi.Order) {
	for i := range m.pendingOrders {
		if m.pendingOrders[i].OrderID == o.OrderID {
			switch o.Status {
			case hibachi.OrderStatusFilled, hibachi.OrderStatusCancelled, hibachi.OrderStatusRejected:
				m.pendingOrders = append(m.pendingOrders[:i], m.pendingOrders[i+1:]...)
			default:
				m.pendingOrders[i] = o
			}
			return
		}
	}
	switch o.Status {
	case hibachi.OrderStatusPending, hibachi.OrderStatusPlaced, hibachi.OrderStatusPartiallyFilled, hibachi.OrderStatusChildPending:
		m.pendingOrders = append(m.pendingOrders, o)
	}
}

func upsertPosition(ps []hibachi.Position, n hibachi.Position) []hibachi.Position {
	for i := range ps {
		if ps[i].Symbol == n.Symbol {
			ps[i] = n
			return ps
		}
	}
	return append(ps, n)
}

func (m Model) applyAdvisor() (tea.Model, tea.Cmd) {
	if m.advisor == nil {
		return m, nil
	}
	m.overlay = OverlayPlanning
	txt := m.advisor.Body
	if m.advisor.Reason != "" {
		txt = txt + "\n\nreason: " + m.advisor.Reason
	}
	return m, planRunCmd(m.deps, txt)
}

// Helpers bound at package scope.

func flashBanner(text string) tea.Cmd {
	return func() tea.Msg {
		return BannerMsg{Text: text, TTL: 3 * time.Second}
	}
}

func manualRefreshCmd(deps Deps) tea.Cmd {
	return func() tea.Msg {
		if deps.Client == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if ai, err := deps.Client.GetAccountInfo(ctx); err == nil {
			return AccountInfoMsg{Info: ai}
		}
		return nil
	}
}

func closePositionCmd(deps Deps, pos hibachi.Position) tea.Cmd {
	return func() tea.Msg {
		if deps.App != nil {
			if err := deps.App.EnsureSigner(); err != nil {
				return ExecuteResultMsg{Err: err}
			}
			deps.Signer = deps.App.Signer
		}
		if deps.App != nil && deps.App.DryRun {
			return BannerMsg{Text: "[dry-run] close " + pos.Symbol, TTL: 3 * time.Second}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		side := hibachi.SideAsk
		if strings.EqualFold(pos.Direction, "Short") {
			side = hibachi.SideBid
		}
		qty, err := parseDec(pos.Quantity)
		if err != nil {
			return ExecuteResultMsg{Err: err}
		}
		maxFees, _ := parseDec("0.0005")
		if _, err := deps.Client.PlaceMarketOrder(ctx, pos.Symbol, side, qty, maxFees); err != nil {
			return ExecuteResultMsg{Err: err}
		}
		return BannerMsg{Text: "close sent: " + pos.Symbol, TTL: 3 * time.Second}
	}
}

func cancelOrderCmd(deps Deps, orderID int64) tea.Cmd {
	return func() tea.Msg {
		if deps.App != nil {
			if err := deps.App.EnsureSigner(); err != nil {
				return ExecuteResultMsg{Err: err}
			}
			deps.Signer = deps.App.Signer
		}
		if deps.App != nil && deps.App.DryRun {
			return BannerMsg{Text: fmt.Sprintf("[dry-run] cancel %d", orderID), TTL: 3 * time.Second}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		id := orderID
		if err := deps.Client.CancelOrder(ctx, hibachi.CancelOrder{OrderID: &id}); err != nil {
			return ExecuteResultMsg{Err: err}
		}
		return BannerMsg{Text: fmt.Sprintf("cancel sent: %d", orderID), TTL: 3 * time.Second}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
