package tui

import (
	"strings"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

var (
	colAccent  = lipgloss.Color("#7D56F4")
	colAccent2 = lipgloss.Color("#5A9BF6")
	colGreen   = lipgloss.Color("#3CB371")
	colYellow  = lipgloss.Color("#E3B341")
	colRed     = lipgloss.Color("#E06C75")
	colCyan    = lipgloss.Color("#56B6C2")
	colMuted   = lipgloss.Color("#7A7A7A")
	colWhite   = lipgloss.Color("#F2F2F2")

	titleBarStyle = lipgloss.NewStyle().Bold(true).Foreground(colWhite).Background(colAccent).Padding(0, 1)
	subtitleStyle = lipgloss.NewStyle().Foreground(colMuted).Italic(true)
	hintStyle     = lipgloss.NewStyle().Foreground(colMuted)
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent2).Padding(0, 1)
	helpKeyStyle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent2)
	helpTextStyle = lipgloss.NewStyle().Foreground(colMuted)

	okStyle      = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(colYellow)
	errStyle     = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	changeStyle  = lipgloss.NewStyle().Foreground(colCyan)
	pendingStyle = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle     = lipgloss.NewStyle().Foreground(colMuted)
	accentStyle  = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "resuelto":
		return okStyle
	case "revisar":
		return warnStyle
	case "error", "descatalogado":
		return errStyle
	case "cambio":
		return changeStyle
	default:
		return pendingStyle
	}
}

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

func pickerStyles() filepicker.Styles {
	s := filepicker.DefaultStyles()
	s.Cursor = s.Cursor.Foreground(colAccent)
	s.Directory = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
	s.File = lipgloss.NewStyle().Foreground(colWhite)
	s.Selected = lipgloss.NewStyle().Foreground(colWhite).Background(colAccent).Bold(true)
	s.DisabledFile = lipgloss.NewStyle().Foreground(colMuted)
	s.Permission = lipgloss.NewStyle().Foreground(colMuted)
	s.FileSize = lipgloss.NewStyle().Foreground(colMuted)
	s.EmptyDirectory = lipgloss.NewStyle().Foreground(colMuted).Italic(true)
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
