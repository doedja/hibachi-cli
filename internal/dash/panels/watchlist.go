package panels

import (
	"fmt"
	"strings"
)

// WatchPrice is the current snapshot for a watchlist entry.
type WatchPrice struct {
	Mark string
	Ask  string
	Bid  string
}

// WatchlistInput is the input for the watchlist panel.
type WatchlistInput struct {
	Styles   Styles
	Width    int
	Height   int
	Focused  bool
	Symbols  []string
	Prices   map[string]WatchPrice
	History  map[string][]float64
	Selected int
}

// Watchlist renders hot symbols with a tiny sparkline.
func Watchlist(in WatchlistInput) string {
	s := in.Styles
	head := title(s, "Watchlist", in.Focused)

	lines := []string{head}
	if len(in.Symbols) == 0 {
		lines = append(lines, s.Dim.Render("  empty"))
	}
	for i, sym := range in.Symbols {
		if i >= 9 {
			break
		}
		p := in.Prices[sym]
		price := "-"
		if p.Mark != "" {
			price = p.Mark
		}
		spark := sparkline(in.History[sym])
		if spark == "" {
			spark = "░░░░░░░░"
		}
		label := fmt.Sprintf("  [%d] %-12s %-12s  %s", i+1, sym, price, spark)
		if i == in.Selected && in.Focused {
			label = s.Selected.Render(label)
		}
		lines = append(lines, label)
	}
	return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(strings.Join(lines, "\n"))
}

var sparkChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func sparkline(history []float64) string {
	if len(history) < 2 {
		return ""
	}
	minV, maxV := history[0], history[0]
	for _, v := range history {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	rangeV := maxV - minV
	if rangeV == 0 {
		return strings.Repeat(string(sparkChars[3]), min(8, len(history)))
	}
	// take last 8 samples.
	start := 0
	if len(history) > 8 {
		start = len(history) - 8
	}
	var b strings.Builder
	for _, v := range history[start:] {
		idx := int((v - minV) / rangeV * float64(len(sparkChars)-1))
		if idx < 0 {
			idx = 0
		}
		if idx > len(sparkChars)-1 {
			idx = len(sparkChars) - 1
		}
		b.WriteRune(sparkChars[idx])
	}
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
