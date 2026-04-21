package panels

import (
	"fmt"
	"strings"

	hibachi "github.com/doedja/hibachi-go"
)

// OrderbookInput is the input for the orderbook panel.
type OrderbookInput struct {
	Styles Styles
	Width  int
	Height int
	Symbol string
	Book   *hibachi.OrderBook
}

// Orderbook renders bid / ask ladders for the focused symbol.
func Orderbook(in OrderbookInput) string {
	s := in.Styles
	head := s.Header.Render(in.Symbol + "  orderbook")
	col := s.ColumnHead.Render(fmt.Sprintf("%-14s %-12s %-14s", "price", "size", "side"))

	lines := []string{head, col}
	if in.Book == nil {
		lines = append(lines, s.Dim.Render("  awaiting orderbook snapshot"))
		body := strings.Join(lines, "\n")
		return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(body)
	}

	asks := in.Book.Ask.Levels
	if len(asks) > 5 {
		asks = asks[:5]
	}
	// render asks top-down (highest first: reverse)
	for i := len(asks) - 1; i >= 0; i-- {
		lv := asks[i]
		lines = append(lines, fmt.Sprintf("  %s %s  %s",
			s.Bad.Render(padRight(lv.Price, 12)),
			padRight(lv.Quantity, 12),
			s.Dim.Render("ask"),
		))
	}

	// Spread row.
	spread := computeSpread(in.Book)
	lines = append(lines, s.Dim.Render("  "+spread))

	bids := in.Book.Bid.Levels
	if len(bids) > 5 {
		bids = bids[:5]
	}
	for _, lv := range bids {
		lines = append(lines, fmt.Sprintf("  %s %s  %s",
			s.Good.Render(padRight(lv.Price, 12)),
			padRight(lv.Quantity, 12),
			s.Dim.Render("bid"),
		))
	}

	body := strings.Join(lines, "\n")
	return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(body)
}

func computeSpread(book *hibachi.OrderBook) string {
	if book == nil {
		return "—"
	}
	if len(book.Ask.Levels) == 0 || len(book.Bid.Levels) == 0 {
		return "—"
	}
	askPrice := book.Ask.Levels[0].Price
	bidPrice := book.Bid.Levels[0].Price
	return fmt.Sprintf("best bid %s  /  best ask %s", bidPrice, askPrice)
}
