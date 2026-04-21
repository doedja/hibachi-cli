package dash

import "github.com/charmbracelet/lipgloss"

// Styles is the shared palette used by every panel.
type Styles struct {
	Border      lipgloss.Style
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
	Overlay     lipgloss.Style
	Input       lipgloss.Style
}

// DefaultStyles returns the default palette.
func DefaultStyles() Styles {
	border := lipgloss.NewStyle().BorderForeground(lipgloss.Color("240"))

	return Styles{
		Border: border,
		Panel: border.
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1),
		Header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")),
		Title:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		Dim:        lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		Muted:      lipgloss.NewStyle().Foreground(lipgloss.Color("248")),
		Accent:     lipgloss.NewStyle().Foreground(lipgloss.Color("213")),
		Good:       lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
		Bad:        lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		Warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		Tag:        lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
		PillLive:   lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("46")).Padding(0, 1).Bold(true),
		PillReconn: lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214")).Padding(0, 1).Bold(true),
		PillOffline: lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("240")).Padding(0, 1).Bold(true),
		PillAI:     lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("213")).Padding(0, 1).Bold(true),
		PillDryRun: lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("220")).Padding(0, 1).Bold(true),
		Footer:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		ColumnHead: lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true),
		Selected:   lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("255")),
		Overlay: border.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("213")).
			Padding(1, 2).
			Background(lipgloss.Color("235")),
		Input: lipgloss.NewStyle().Foreground(lipgloss.Color("255")),
	}
}
