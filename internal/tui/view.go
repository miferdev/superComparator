package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/report"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func (m *Model) view() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("SuperComparator · Mercadona vs Ahorramas"))
	b.WriteString("\n\n")

	switch m.screen {
	case screenPicker:
		b.WriteString("Elige tu lista de la compra (tabla markdown de dos columnas):\n\n")
		b.WriteString(m.picker.View())
	case screenList:
		if len(m.items) == 0 {
			b.WriteString("No hay productos en la lista.\n")
		} else {
			b.WriteString(m.table.View())
		}
	case screenProgress:
		fmt.Fprintf(&b, "%s Trabajando…\n\n", m.spinner.View())
		b.WriteString(strings.Join(m.logLines, "\n"))
	case screenCompare:
		if len(m.cmp.Items) == 0 {
			b.WriteString("No hay productos resueltos todavía. Pulsa 'r' en la lista para resolver.\n")
		} else {
			b.WriteString(m.table.View())
			b.WriteString("\n")
			b.WriteString(m.totalsView())
			fmt.Fprintf(&b, "\nInforme escrito en %s\n", m.cfg.ReportPath)
		}
	case screenHistory:
		if len(m.changes) == 0 {
			b.WriteString("No hay cambios de precio registrados todavía.\n")
		} else {
			b.WriteString(m.table.View())
		}
	case screenReview:
		fmt.Fprintf(&b, "Alternativas para %q:\n\n", m.review)
		for i, opt := range m.options {
			fmt.Fprintf(&b, "  %d. [%s] %s  (%.2f)\n", i+1, report.ChainName(opt.ChainID), opt.Name, opt.Score)
		}
	}

	if m.message != "" {
		b.WriteString("\n" + infoStyle.Render(m.message) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render(m.help()))
	return b.String()
}

func (m *Model) help() string {
	switch m.screen {
	case screenPicker:
		return "enter: elegir fichero · ↑/↓: moverse · q: salir"
	case screenList:
		return "r: resolver · c: comparar · h: historial · enter: alternativas · q: salir"
	case screenProgress:
		return "resolviendo, espera…"
	case screenCompare:
		return "esc o q: volver a la lista"
	case screenHistory:
		return "esc o q: volver a la lista"
	case screenReview:
		return "1-9: elegir alternativa · esc: volver"
	}
	return ""
}

func (m *Model) totalsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Totales") + "\n")
	for _, chainID := range m.cmp.Chains {
		if total, ok := m.cmp.Totals[chainID]; ok {
			fmt.Fprintf(&b, "  %-12s %s\n", report.ChainName(chainID), money(total))
		}
	}
	fmt.Fprintf(&b, "  %-12s %s\n", "Mixta", money(m.cmp.MixedTotal))
	if m.cmp.CheapestChain != "" {
		fmt.Fprintf(&b, "\n  Cadena más barata: %s (ahorro %s)\n",
			report.ChainName(m.cmp.CheapestChain), money(m.cmp.MaxSaving))
	}
	return b.String()
}

func (m *Model) refreshItems() {
	rows := make([]table.Row, 0, len(m.items))
	for _, it := range m.items {
		rows = append(rows, table.Row{it.Name, fmt.Sprintf("%d", it.Quantity), m.status[it.Name], m.notes[it.Name]})
	}
	m.table.SetRows(nil)
	m.table.SetColumns([]table.Column{
		{Title: "Producto", Width: clamp(m.width*35/100, 20, 60)},
		{Title: "Cant.", Width: 6},
		{Title: "Estado", Width: 14},
		{Title: "Último match", Width: clamp(m.width*40/100, 24, 80)},
	})
	m.table.SetRows(rows)
	m.table.SetCursor(0)
}

func (m *Model) refreshCompare() {
	rows := make([]table.Row, 0, len(m.cmp.Items))
	for _, item := range m.cmp.Items {
		row := table.Row{item.Name, fmt.Sprintf("%d", item.Quantity)}
		for _, chainID := range m.cmp.Chains {
			row = append(row, optionCell(item, chainID))
		}
		row = append(row, report.ChainName(item.Cheapest))
		rows = append(rows, row)
	}
	cols := []table.Column{
		{Title: "Producto", Width: clamp(m.width*28/100, 18, 50)},
		{Title: "Cant.", Width: 6},
	}
	for _, chainID := range m.cmp.Chains {
		cols = append(cols, table.Column{Title: report.ChainName(chainID), Width: clamp(m.width*24/100, 18, 50)})
	}
	cols = append(cols, table.Column{Title: "Más barato", Width: 12})
	m.table.SetRows(nil)
	m.table.SetColumns(cols)
	m.table.SetRows(rows)
	m.table.SetCursor(0)
}

func optionCell(item core.ItemComparison, chainID string) string {
	for _, opt := range item.Options {
		if opt.Chain != chainID || opt.Price <= 0 {
			continue
		}
		cell := money(opt.Price)
		if opt.MeasurePrice > 0 {
			cell += fmt.Sprintf(" · %s/%s", money(opt.MeasurePrice), opt.MeasureUnit)
		}
		if opt.Promo {
			cell += " · oferta"
		}
		return cell
	}
	return "—"
}

func (m *Model) refreshHistory() {
	rows := make([]table.Row, 0, len(m.changes))
	for _, c := range m.changes {
		rows = append(rows, table.Row{
			report.ChainName(c.Chain),
			c.Name,
			money(c.OldPrice),
			money(c.NewPrice),
			c.FetchedAt.Format(time.DateTime),
		})
	}
	m.table.SetRows(nil)
	m.table.SetColumns([]table.Column{
		{Title: "Cadena", Width: 12},
		{Title: "Producto", Width: clamp(m.width*40/100, 24, 70)},
		{Title: "Antes", Width: 12},
		{Title: "Ahora", Width: 12},
		{Title: "Fecha", Width: 20},
	})
	m.table.SetRows(rows)
	m.table.SetCursor(0)
}

func keyOf(msg tea.Msg) (string, bool) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return k.String(), true
	}
	return "", false
}

func keyNumber(key string) int {
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		return int(key[0] - '1')
	}
	return -1
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func money(v float64) string {
	return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1) + " €"
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(lipgloss.Color("111"))
	s.Selected = s.Selected.Foreground(lipgloss.Color("229"))
	return s
}
