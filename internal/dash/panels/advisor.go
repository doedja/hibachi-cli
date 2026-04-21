package panels

import (
	"strings"
)

// AdvisorInput is the input for the advisor panel.
type AdvisorInput struct {
	Styles  Styles
	Width   int
	Height  int
	Focused bool
	Enabled bool
	Present bool
	Body    string
	Reason  string
	Session string
	At      string
}

// Advisor renders the AI advisor suggestion card.
func Advisor(in AdvisorInput) string {
	s := in.Styles
	title := "Advisor"
	if in.Session != "" || in.At != "" {
		title += "  " + s.Muted.Render("session "+trimSession(in.Session)+" · "+in.At)
	}
	head := s.Header.Render(title) + "  " + s.PillAI.Render("ai")

	lines := []string{head}

	if !in.Enabled {
		lines = append(lines, s.Dim.Render("  advisor panel hidden ([a] to toggle)"))
		return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(strings.Join(lines, "\n"))
	}
	if !in.Present {
		lines = append(lines, s.Dim.Render("  no advisor running"))
		return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(strings.Join(lines, "\n"))
	}

	lines = append(lines, "  "+s.Tag.Render("suggestion")+"  "+in.Body)
	if in.Reason != "" {
		lines = append(lines, "  "+s.Dim.Render("reason")+"      "+in.Reason)
	}
	lines = append(lines, "  "+s.Tag.Render("[a]")+" apply   "+s.Tag.Render("[i]")+" ignore   "+s.Tag.Render("[m]")+" modify")
	return s.Panel.Width(in.Width - 2).Height(in.Height - 2).Render(strings.Join(lines, "\n"))
}

func trimSession(s string) string {
	if s == "" {
		return "-"
	}
	if len(s) > 20 {
		return s[:20]
	}
	return s
}
