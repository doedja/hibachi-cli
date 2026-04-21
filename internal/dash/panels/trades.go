package panels

import (
	"fmt"
	"strings"
	"time"
)

// Trade is a panel-local trade record (decouples from WS-specific types).
type Trade struct {
	Symbol    string
	Price     string
	Quantity  string
	Taker     string
	Timestamp int64
}

// TradesInput is the input for the recent trades panel.
type TradesInput struct {
	Styles  Styles
	Width   int
	Height  int
	Focused bool
	Trades  []Trade
}

// Trades renders the recent-trade feed.
func Trades(in TradesInput) string {
	s := in.Styles
	head := title(s, "Recent trades", in.Focused)
	col := s.ColumnHead.Render(fmt.Sprintf("%-10s %-4s %-12s %-10s", "time", "side", "qty", "price"))

	lines := []string{head, col}
	if len(in.Trades) == 0 {
		lines = append(lines, s.Dim.Render("  awaiting trades"))
	}
	max := in.Height - 4
	if max < 1 {
		max = 1
	}
	for i, t := range in.Trades {
		if i >= max {
			break
		}
		taker := t.Taker
		sideStyle := s.Good
		label := "buy"
		if taker == "Sell" {
			sideStyle = s.Bad
			label = "sell"
		}
		ts := "-"
		if t.Timestamp > 0 {
			ts = time.UnixMilli(t.Timestamp).UTC().Format("15:04:05")
		}
		row := fmt.Sprintf("  %-10s %s %-12s %-10s",
			ts,
			sideStyle.Render(padRight(label, 4)),
			t.Quantity,
			t.Price,
		)
		lines = append(lines, row)
	}
	return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(strings.Join(lines, "\n"))
}
