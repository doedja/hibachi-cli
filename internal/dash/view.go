package dash

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/doedja/hibachi-cli/internal/dash/panels"
)

// View composes the full dashboard frame.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading dash..."
	}

	// Header.
	header := panels.Header(panels.HeaderInput{
		Styles:        panelsStyles(m.styles),
		AccountID:     m.deps.Cfg.API.AccountID,
		Now:           m.now,
		Backend:       backendLabel(m),
		MarketStatus:  m.marketStatus.String(),
		AccountStatus: m.accountStatus.String(),
		Width:         m.width,
		DryRun:        m.deps.App != nil && m.deps.App.DryRun,
		BalanceLine:   balanceLine(m),
	})

	// Footer.
	footer := panels.Footer(panels.FooterInput{
		Styles: panelsStyles(m.styles),
		Banner: m.banner,
		Width:  m.width,
	})

	// Content area.
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 10 {
		contentHeight = 10
	}

	left := m.renderLeft(contentHeight)
	right := m.renderRight(contentHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	root := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	// Overlay compositing.
	switch m.overlay {
	case OverlayPrompt:
		return overlayOn(root, m.renderPromptOverlay(), m.width, m.height)
	case OverlayPlanning:
		return overlayOn(root, m.renderPlanningOverlay(), m.width, m.height)
	case OverlayPlan:
		return overlayOn(root, m.renderPlanOverlay(), m.width, m.height)
	case OverlayConfirmClose:
		return overlayOn(root, m.renderConfirm("Close position?", closeSummary(m)), m.width, m.height)
	case OverlayConfirmCancel:
		return overlayOn(root, m.renderConfirm("Cancel order?", cancelSummary(m)), m.width, m.height)
	case OverlayHelp:
		return overlayOn(root, m.renderHelpOverlay(), m.width, m.height)
	case OverlayError:
		return overlayOn(root, m.renderErrorOverlay(), m.width, m.height)
	}

	return root
}

func (m Model) renderLeft(height int) string {
	leftWidth := m.width / 2
	if leftWidth < 40 {
		leftWidth = 40
	}
	// Divide vertical into positions (30%), orders (25%), trades (45%).
	posH := clamp(height*30/100, 5, height-10)
	ordH := clamp(height*25/100, 4, height-posH-5)
	trdH := height - posH - ordH
	if trdH < 5 {
		trdH = 5
	}

	positions := panels.Positions(panels.PositionsInput{
		Styles:   panelsStyles(m.styles),
		Width:    leftWidth,
		Height:   posH,
		Focused:  m.focus == FocusPositions,
		Positions: m.positions(),
		Selected: m.positionIdx,
	})
	orders := panels.Orders(panels.OrdersInput{
		Styles:   panelsStyles(m.styles),
		Width:    leftWidth,
		Height:   ordH,
		Focused:  m.focus == FocusOrders,
		Orders:   m.pendingOrders,
		Selected: m.orderIdx,
		Now:      m.now,
	})
	trades := panels.Trades(panels.TradesInput{
		Styles:  panelsStyles(m.styles),
		Width:   leftWidth,
		Height:  trdH,
		Focused: m.focus == FocusTrades,
		Trades:  ringToPanel(m.recentTrades),
	})
	return lipgloss.JoinVertical(lipgloss.Left, positions, orders, trades)
}

func (m Model) renderRight(height int) string {
	rightWidth := m.width - (m.width / 2)
	if rightWidth < 40 {
		rightWidth = 40
	}
	obH := clamp(height*50/100, 10, height-10)
	wlH := clamp(height*25/100, 4, height-obH-5)
	advH := height - obH - wlH
	if advH < 4 {
		advH = 4
	}

	ob := panels.Orderbook(panels.OrderbookInput{
		Styles:  panelsStyles(m.styles),
		Width:   rightWidth,
		Height:  obH,
		Symbol:  m.focusedSymbol,
		Book:    m.orderbook,
	})
	wl := panels.Watchlist(panels.WatchlistInput{
		Styles:  panelsStyles(m.styles),
		Width:   rightWidth,
		Height:  wlH,
		Focused: m.focus == FocusWatchlist,
		Symbols: m.watchlist,
		Prices:  priceMap(m.watchPrices),
		History: m.watchHistory,
		Selected: m.watchIdx,
	})
	adv := panels.Advisor(panels.AdvisorInput{
		Styles:  panelsStyles(m.styles),
		Width:   rightWidth,
		Height:  advH,
		Focused: m.focus == FocusAdvisor,
		Enabled: m.advisorOn,
		Present: m.advisor != nil,
		Body: func() string {
			if m.advisor == nil {
				return ""
			}
			return m.advisor.Body
		}(),
		Reason: func() string {
			if m.advisor == nil {
				return ""
			}
			return m.advisor.Reason
		}(),
		Session: func() string {
			if m.advisor == nil {
				return ""
			}
			return m.advisor.Session
		}(),
		At: func() string {
			if m.advisor == nil {
				return ""
			}
			return m.advisor.At.Format("15:04:05")
		}(),
	})
	return lipgloss.JoinVertical(lipgloss.Left, ob, wl, adv)
}

func (m Model) renderPromptOverlay() string {
	w := clamp(m.width*3/5, 40, m.width-8)
	title := m.styles.Title.Render("/") + " " + m.styles.Muted.Render("prompt") + " " + m.styles.PillAI.Render("ai")
	cursor := "_"
	body := m.styles.Accent.Render("› ") + m.prompt + m.styles.Dim.Render(cursor)
	help := m.styles.Dim.Render("enter to plan   esc to cancel")
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", help)
	return m.styles.Overlay.Width(w).Render(inner)
}

func (m Model) renderPlanningOverlay() string {
	w := clamp(m.width*3/5, 40, m.width-8)
	inner := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.Title.Render("planning..."),
		"",
		m.styles.Muted.Render("calling "+backendLabel(m)),
		m.styles.Dim.Render("prompt: "+truncate(m.prompt, 120)),
	)
	return m.styles.Overlay.Width(w).Render(inner)
}

func (m Model) renderPlanOverlay() string {
	w := clamp(m.width*3/5, 50, m.width-8)
	if m.lastPlan == nil {
		return m.styles.Overlay.Width(w).Render("no plan")
	}
	var b strings.Builder
	b.WriteString(m.styles.Title.Render("plan") + "\n")
	for _, a := range m.lastPlan.Actions {
		line := a.Kind
		if a.Symbol != "" {
			line += "  " + a.Symbol
		}
		if a.Side != "" {
			line += "  " + a.Side
		}
		if a.Qty != "" {
			line += "  qty " + a.Qty
		}
		if a.Price != "" {
			line += "  @ " + a.Price
		}
		b.WriteString("  " + line + "\n")
		if a.Reason != "" {
			b.WriteString("    " + m.styles.Dim.Render(a.Reason) + "\n")
		}
	}
	if m.lastPlan.Reasoning != "" {
		b.WriteString("\n" + m.styles.Muted.Render(m.lastPlan.Reasoning) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.styles.Dim.Render("submit? [y] / n / e(dit) / d(ump)"))
	return m.styles.Overlay.Width(w).Render(b.String())
}

func (m Model) renderConfirm(title, msg string) string {
	w := clamp(m.width/2, 40, m.width-8)
	inner := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.Title.Render(title),
		"",
		msg,
		"",
		m.styles.Dim.Render("[y] yes   [n] no"),
	)
	return m.styles.Overlay.Width(w).Render(inner)
}

func (m Model) renderHelpOverlay() string {
	w := clamp(m.width*3/5, 50, m.width-8)
	lines := []string{
		m.styles.Title.Render("keys"),
		"",
		"q          quit",
		"tab        cycle focus",
		"/          prompt",
		"b / s      buy / sell (v0.2 stub)",
		"c          close focused position",
		"x          cancel focused order",
		"a          advisor toggle / apply when suggestion present",
		"i          ignore advisor suggestion",
		"r          force REST refresh",
		"1-9        select watchlist entry",
		"up / down  move selection",
		"?          this help",
		"esc        close overlay",
	}
	return m.styles.Overlay.Width(w).Render(strings.Join(lines, "\n"))
}

func (m Model) renderErrorOverlay() string {
	w := clamp(m.width*3/5, 40, m.width-8)
	inner := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.Bad.Render("error"),
		"",
		m.lastError,
		"",
		m.styles.Dim.Render("press esc to dismiss"),
	)
	return m.styles.Overlay.Width(w).Render(inner)
}

func overlayOn(_, fg string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, fg, lipgloss.WithWhitespaceChars(" "))
}

func backendLabel(m Model) string {
	if m.deps.Cfg == nil {
		return ""
	}
	backend := m.deps.Cfg.AI.Backend
	model := ""
	switch backend {
	case "openrouter":
		model = m.deps.Cfg.AI.OpenRouter.Model
	default:
		model = m.deps.Cfg.AI.ClaudeCode.Model
	}
	if model == "" {
		return backend
	}
	return backend + " · " + model
}

func balanceLine(m Model) string {
	if m.accountStatus == ConnDisabled {
		return "account streaming disabled (auth login to enable)"
	}
	bal, notional := "-", "-"
	if m.accountInfo != nil {
		if m.accountInfo.Balance != "" {
			bal = m.accountInfo.Balance
		}
		if m.accountInfo.TotalPositionNotional != "" {
			notional = m.accountInfo.TotalPositionNotional
		}
	}
	return "balance " + bal + "   total notional " + notional
}

func closeSummary(m Model) string {
	pos := m.focusedPosition()
	if pos == nil {
		return "no position selected"
	}
	return pos.Symbol + "  " + pos.Direction + "  qty " + pos.Quantity + "  mark " + pos.MarkPrice
}

func cancelSummary(m Model) string {
	ord := m.focusedOrder()
	if ord == nil {
		return "no order selected"
	}
	price := "-"
	if ord.Price != nil {
		price = *ord.Price
	}
	return ord.Symbol + "  " + string(ord.Side) + "  price " + price + "  qty " + ord.AvailableQuantity
}

// panelsStyles packs the palette into the panels.Styles shape.
func panelsStyles(s Styles) panels.Styles {
	return panels.Styles{
		Panel:       s.Panel,
		Header:      s.Header,
		Title:       s.Title,
		Dim:         s.Dim,
		Muted:       s.Muted,
		Accent:      s.Accent,
		Good:        s.Good,
		Bad:         s.Bad,
		Warn:        s.Warn,
		Tag:         s.Tag,
		PillLive:    s.PillLive,
		PillReconn:  s.PillReconn,
		PillOffline: s.PillOffline,
		PillAI:      s.PillAI,
		PillDryRun:  s.PillDryRun,
		Footer:      s.Footer,
		ColumnHead:  s.ColumnHead,
		Selected:    s.Selected,
	}
}

func ringToPanel(rs []RecentTrade) []panels.Trade {
	out := make([]panels.Trade, len(rs))
	for i, r := range rs {
		out[i] = panels.Trade{
			Symbol:    r.Symbol,
			Price:     r.Price,
			Quantity:  r.Quantity,
			Taker:     r.Taker,
			Timestamp: r.Timestamp,
		}
	}
	return out
}

func priceMap(in map[string]PriceTickMsg) map[string]panels.WatchPrice {
	out := make(map[string]panels.WatchPrice, len(in))
	for k, v := range in {
		out[k] = panels.WatchPrice{Mark: v.Mark, Ask: v.Ask, Bid: v.Bid}
	}
	return out
}
