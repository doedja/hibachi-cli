package panels

import (
	"fmt"
	"strings"
	"time"

	hibachi "github.com/doedja/hibachi-go"
)

// OrdersInput is the input for the open orders panel.
type OrdersInput struct {
	Styles   Styles
	Width    int
	Height   int
	Focused  bool
	Orders   []hibachi.Order
	Selected int
	Now      time.Time
}

// Orders renders the pending orders table.
func Orders(in OrdersInput) string {
	s := in.Styles
	head := title(s, "Open orders", in.Focused)
	col := s.ColumnHead.Render(fmt.Sprintf("%-10s %-12s %-5s %-12s %-10s %-8s", "id", "sym", "side", "price", "size", "age"))

	lines := []string{head, col}
	if len(in.Orders) == 0 {
		lines = append(lines, s.Dim.Render("  no open orders"))
	}
	for i, o := range in.Orders {
		side := string(o.Side)
		sideStyle := s.Good
		if side == "ASK" || side == "SELL" {
			sideStyle = s.Bad
		}
		price := "-"
		if o.Price != nil {
			price = *o.Price
		}
		age := "-"
		if o.CreationTime != nil {
			age = formatAge(*o.CreationTime, in.Now)
		}
		row := fmt.Sprintf("%-10d %-12s %s %-12s %-10s %-8s",
			o.OrderID,
			o.Symbol,
			sideStyle.Render(padRight(side, 5)),
			price,
			o.AvailableQuantity,
			age,
		)
		if i == in.Selected && in.Focused {
			row = s.Selected.Render(row)
		}
		lines = append(lines, "  "+row)
	}
	body := strings.Join(lines, "\n")
	return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(body)
}

func formatAge(createdMS int64, now time.Time) string {
	created := time.UnixMilli(createdMS)
	d := now.Sub(created)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
