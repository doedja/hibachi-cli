package panels

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterInput is the input for the bottom status strip.
type FooterInput struct {
	Styles Styles
	Banner string
	Width  int
}

// Footer renders the keybinding legend plus any flash banner.
func Footer(in FooterInput) string {
	s := in.Styles
	keys := []string{
		"[q] quit",
		"[tab] focus",
		"[/] prompt",
		"[b] buy",
		"[s] sell",
		"[c] close",
		"[x] cancel",
		"[a] advisor",
		"[r] refresh",
		"[?] help",
	}
	legend := s.Footer.Render(strings.Join(keys, "   "))
	if in.Banner != "" {
		legend += "   " + s.Warn.Render(in.Banner)
	}
	w := in.Width - 2
	if w < 10 {
		w = 10
	}
	return lipgloss.NewStyle().Width(w).Render(legend)
}
