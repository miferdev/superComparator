package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

var (
	colAccent  = lipgloss.Color("#7D56F4")
	colAccent2 = lipgloss.Color("#5A9BF6")
	colGreen   = lipgloss.Color("#3CB371")
	colYellow  = lipgloss.Color("#E3B341")
	colRed     = lipgloss.Color("#E06C75")
	colMuted   = lipgloss.Color("#7A7A7A")
	colWhite   = lipgloss.Color("#F2F2F2")

	titleBarStyle    = lipgloss.NewStyle().Bold(true).Foreground(colWhite).Background(colAccent).Padding(0, 1)
	selectedRowStyle = lipgloss.NewStyle().Bold(true).Foreground(colWhite).Background(colAccent).Padding(0, 1)
	subtitleStyle    = lipgloss.NewStyle().Foreground(colMuted).Italic(true)
	hintStyle        = lipgloss.NewStyle().Foreground(colMuted)
	boxStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent2).Padding(0, 1)
	helpKeyStyle     = lipgloss.NewStyle().Bold(true).Foreground(colAccent2)
	helpTextStyle    = lipgloss.NewStyle().Foreground(colMuted)

	okStyle     = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(colYellow)
	errStyle    = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(colMuted)
	accentStyle = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
)

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(colAccent2).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderBottomForeground(colMuted)
	s.Cell = s.Cell.Padding(0, 1)
	s.Selected = s.Selected.Foreground(colWhite).Background(colAccent).Bold(true)
	return s
}

type helpBinding struct {
	key  string
	desc string
}

func helpLine(bindings ...helpBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, helpKeyStyle.Render(b.key)+" "+helpTextStyle.Render(b.desc))
	}
	return strings.Join(parts, helpTextStyle.Render("  ·  "))
}
