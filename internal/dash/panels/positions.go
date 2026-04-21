package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	hibachi "github.com/doedja/hibachi-go"
)

// PositionsInput is the input for the positions panel.
type PositionsInput struct {
	Styles    Styles
	Width     int
	Height    int
	Focused   bool
	Positions []hibachi.Position
	Selected  int
}

// Positions renders the open positions table.
func Positions(in PositionsInput) string {
	s := in.Styles
	head := title(s, "Positions", in.Focused)

	col := s.ColumnHead.Render(fmt.Sprintf("%-12s %-6s %-10s %-12s %-12s %-10s", "symbol", "side", "size", "entry", "mark", "pnl"))

	lines := []string{head, col}
	if len(in.Positions) == 0 {
		lines = append(lines, s.Dim.Render("  no open positions"))
	}
	for i, p := range in.Positions {
		side := p.Direction
		sideStyle := s.Good
		if strings.EqualFold(side, "Short") {
			sideStyle = s.Bad
		}
		pnl := p.UnrealizedTradingPnl
		pnlStyle := s.Good
		if strings.HasPrefix(strings.TrimSpace(pnl), "-") {
			pnlStyle = s.Bad
		}
		row := fmt.Sprintf("%-12s %s %-10s %-12s %-12s %s",
			p.Symbol,
			sideStyle.Render(padRight(upper(side), 6)),
			p.Quantity,
			p.OpenPrice,
			p.MarkPrice,
			pnlStyle.Render(pnl),
		)
		if i == in.Selected && in.Focused {
			row = s.Selected.Render(row)
		}
		lines = append(lines, "  "+row)
	}

	body := strings.Join(lines, "\n")
	return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(body)
}

func title(s Styles, label string, focused bool) string {
	if focused {
		return s.Title.Render("▶ "+label) + "   " + s.Dim.Render("")
	}
	return s.Header.Render(label)
}

func upper(s string) string { return strings.ToUpper(s) }

func padRight(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}
