package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Styles is a slim subset of dash.Styles threaded through panels.
type Styles struct {
	Panel       lipgloss.Style
	Header      lipgloss.Style
	Title       lipgloss.Style
	Dim         lipgloss.Style
	Muted       lipgloss.Style
	Accent      lipgloss.Style
	Good        lipgloss.Style
	Bad         lipgloss.Style
	Warn        lipgloss.Style
	Tag         lipgloss.Style
	PillLive    lipgloss.Style
	PillReconn  lipgloss.Style
	PillOffline lipgloss.Style
	PillAI      lipgloss.Style
	PillDryRun  lipgloss.Style
	Footer      lipgloss.Style
	ColumnHead  lipgloss.Style
	Selected    lipgloss.Style
}

// HeaderInput is the input for the top strip.
type HeaderInput struct {
	Styles        Styles
	AccountID     int
	Now           time.Time
	Backend       string
	MarketStatus  string
	AccountStatus string
	Width         int
	DryRun        bool
	BalanceLine   string
}

// Header renders the top strip: identity, clock, backend, status pill, balances.
func Header(in HeaderInput) string {
	s := in.Styles
	ts := in.Now.Format("15:04:05 UTC")

	acctLabel := "acct -"
	if in.AccountID != 0 {
		acctLabel = fmt.Sprintf("acct %d", in.AccountID)
	}

	pill := pillFor(s, in.MarketStatus, in.AccountStatus)
	dry := ""
	if in.DryRun {
		dry = " " + s.PillDryRun.Render("DRY-RUN")
	}

	line1 := strings.Join([]string{
		s.Title.Render("hibachi"),
		s.Muted.Render(acctLabel),
		s.Muted.Render(ts),
		s.Accent.Render(in.Backend),
		pill + dry,
	}, "  ")

	line2 := s.Muted.Render(in.BalanceLine)

	body := line1 + "\n" + line2
	return s.Panel.Width(in.Width - 2).Render(body)
}

func pillFor(s Styles, market, account string) string {
	// Worst of the two wins.
	switch {
	case market == "reconnecting" || account == "reconnecting":
		return s.PillReconn.Render("reconnecting")
	case market == "error" || account == "error":
		return s.PillOffline.Render("offline")
	case market == "live" && (account == "live" || account == "disabled"):
		return s.PillLive.Render("live")
	case market == "connecting" || account == "connecting":
		return s.PillReconn.Render("connecting")
	}
	return s.PillOffline.Render(market)
}
