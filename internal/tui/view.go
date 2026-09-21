package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/report"
)

func (m *Model) view() string {
	var b strings.Builder
	b.WriteString(titleBarStyle.Render(" SuperComparator "))
	b.WriteString(" ")
	b.WriteString(subtitleStyle.Render("Mercadona vs Ahorramas"))
	b.WriteString("\n")
	if hint := m.screenHint(); hint != "" {
		b.WriteString(hintStyle.Render(hint))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	switch m.screen {
	case screenPicker:
		b.WriteString(m.browser.view())
	case screenList:
		if len(m.items) == 0 {
			b.WriteString(dimStyle.Render("No hay productos en la lista."))
		} else {
			b.WriteString(m.table.View())
			b.WriteString("\n")
			b.WriteString(m.itemsTotalView())
		}
	case screenProgress:
		fmt.Fprintf(&b, "%s %s\n\n", m.spinner.View(), accentStyle.Render("Trabajando…"))
		for i, line := range m.logLines {
			style := dimStyle
			if i == len(m.logLines)-1 {
				style = accentStyle
			}
			b.WriteString(style.Render(line) + "\n")
		}
	case screenCompare:
		if len(m.cmp.Items) == 0 {
			b.WriteString(dimStyle.Render("No hay productos resueltos todavía. Pulsa 'r' en la lista para resolver."))
		} else {
			b.WriteString(m.table.View())
			b.WriteString("\n")
			b.WriteString(m.totalsView())
		}
	case screenHistory:
		if len(m.changes) == 0 {
			b.WriteString(dimStyle.Render("No hay cambios de precio registrados todavía."))
		} else {
			b.WriteString(m.table.View())
		}
	case screenReview:
		b.WriteString(dimStyle.Render(fmt.Sprintf("Alternativas para %q:", m.review)) + "\n")
		b.WriteString(dimStyle.Render("Precio unidad · precio por kg/l") + "\n\n")
		cheapest, criterion := core.CheapestIndex(m.reviewOptions())
		for i, opt := range m.options {
			cursor := "  "
			if i == m.reviewCursor {
				cursor = accentStyle.Render("▶ ")
			}
			num := helpKeyStyle.Render(fmt.Sprintf("%d", i+1))
			chain := accentStyle.Render(report.ChainName(opt.ChainID))
			price := money(opt.Price)
			if opt.MeasurePrice > 0 {
				price += fmt.Sprintf(" · %s/%s", money(opt.MeasurePrice), opt.MeasureUnit)
			}
			mark := ""
			if i == cheapest {
				mark = "  " + okStyle.Render("← más barato ("+criterion+")")
			}
			fmt.Fprintf(&b, "%s%s  %s  %s\n", cursor, num, chain, opt.Name)
			fmt.Fprintf(&b, "      %s  ·  confianza %.2f%s\n", price, opt.Score, mark)
		}
	}

	if m.message != "" {
		b.WriteString("\n" + hintStyle.Render(m.message) + "\n")
	}
	b.WriteString("\n" + m.helpView())
	return b.String()
}

func (m *Model) screenHint() string {
	switch m.screen {
	case screenPicker:
		return "Elige tu lista de la compra (tabla markdown de dos columnas)"
	case screenList:
		return fmt.Sprintf("%d productos · pulsa r para resolver y c para comparar", len(m.items))
	case screenProgress:
		return "Consultando Mercadona y Ahorramas"
	case screenCompare:
		return "Totales por cadena y opción más barata de cada producto"
	case screenHistory:
		return "Cambios de precio y descatalogados"
	case screenReview:
		return "Elige otra opción para este producto"
	}
	return ""
}

func (m *Model) helpView() string {
	switch m.screen {
	case screenPicker:
		return helpLine(
			helpBinding{"enter", "abrir/elegir"},
			helpBinding{"↑/↓", "moverse"},
			helpBinding{"←", "subir"},
			helpBinding{"q", "salir"},
		)
	case screenList:
		return helpLine(
			helpBinding{"r", "resolver"},
			helpBinding{"c", "comparar"},
			helpBinding{"h", "historial"},
			helpBinding{"enter", "alternativas"},
			helpBinding{"q", "salir"},
		)
	case screenProgress:
		return helpLine(helpBinding{"ctrl+c", "salir"})
	case screenCompare:
		return helpLine(helpBinding{"↑/↓", "moverse"}, helpBinding{"esc", "volver"}, helpBinding{"q", "volver"}, helpBinding{"ctrl+c", "salir"})
	case screenHistory:
		return helpLine(helpBinding{"↑/↓", "moverse"}, helpBinding{"esc", "volver"}, helpBinding{"q", "volver"}, helpBinding{"ctrl+c", "salir"})
	case screenReview:
		return helpLine(
			helpBinding{"↑/↓", "moverse"},
			helpBinding{"enter", "elegir"},
			helpBinding{"1-9", "elegir"},
			helpBinding{"esc", "volver"},
		)
	}
	return ""
}

func (m *Model) totalsView() string {
	var lines []string
	for _, chainID := range m.cmp.Chains {
		total, ok := m.cmp.Totals[chainID]
		if !ok || total == 0 {
			continue
		}
		label := fmt.Sprintf("%-12s %8s", report.ChainName(chainID), money(total))
		if chainID == m.cmp.CheapestChain {
			lines = append(lines, okStyle.Render(label+"  ← más barata"))
			continue
		}
		lines = append(lines, label)
	}
	lines = append(lines, fmt.Sprintf("%-12s %8s", "Mixta", money(m.cmp.MixedTotal)))

	out := boxStyle.Render(strings.Join(lines, "\n"))
	if m.cmp.CheapestChain != "" && m.cmp.MaxSaving > 0 {
		out += "\n" + hintStyle.Render("Ahorro máximo: ") + okStyle.Render(money(m.cmp.MaxSaving))
	}
	return out
}

func (m *Model) refreshItems() {
	rows := make([]table.Row, 0, len(m.items))
	for _, it := range m.items {
		rows = append(rows, table.Row{
			it.Name,
			fmt.Sprintf("%d", it.Quantity),
			statusEmoji(m.status[it.Name]),
			lineTotalCell(m.lineTotals[it.Name]),
			dimStyle.Render(m.notes[it.Name]),
		})
	}
	m.table.SetRows(nil)
	m.table.SetColumns([]table.Column{
		{Title: "Producto", Width: clamp(m.width*30/100, 18, 50)},
		{Title: "Cant.", Width: 6},
		{Title: "OK", Width: 4},
		{Title: "Total", Width: 12},
		{Title: "Último match", Width: clamp(m.width*32/100, 20, 70)},
	})
	m.table.SetHeight(clamp(len(rows)+2, 3, clamp(m.height-16, 5, 50)))
	m.table.SetRows(rows)
	m.table.SetCursor(0)
}

// statusEmoji resume el estado en un tick verde (resuelto) o una equis roja.
func statusEmoji(status string) string {
	if status == "resuelto" {
		return okStyle.Render("✅")
	}
	return errStyle.Render("❌")
}

func lineTotalCell(total float64) string {
	if total <= 0 {
		return dimStyle.Render("—")
	}
	return money(total)
}

// itemsTotalView muestra el coste total de la compra con dígitos grandes.
func (m *Model) itemsTotalView() string {
	label := hintStyle.Render(totalIndent + "TOTAL COMPRA")
	if m.grandTotal <= 0 {
		return label + "  " + dimStyle.Render("—") + "\n"
	}
	return label + "\n" + bigMoney(m.grandTotal) + "\n"
}

func (m *Model) refreshCompare() {
	rows := make([]table.Row, 0, len(m.cmp.Items))
	for _, item := range m.cmp.Items {
		row := table.Row{item.Name, fmt.Sprintf("%d", item.Quantity)}
		for _, chainID := range m.cmp.Chains {
			row = append(row, optionCell(item, chainID))
		}
		row = append(row, okStyle.Render(report.CheapestLabel(item)))
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
	m.table.SetHeight(clamp(m.height-14, 5, 52))
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
		cell := opt.Product
		if opt.MeasurePrice > 0 {
			cell += fmt.Sprintf(" (%s · %s/%s)", money(opt.Price), money(opt.MeasurePrice), opt.MeasureUnit)
		} else {
			cell += fmt.Sprintf(" (%s)", money(opt.Price))
		}
		if opt.Promo {
			cell += " · oferta"
		}
		if opt.Stale {
			cell += " · desactualizado"
		}
		if opt.Chain == item.Cheapest {
			return okStyle.Render(cell)
		}
		if opt.Promo {
			return warnStyle.Render(cell)
		}
		return cell
	}
	return dimStyle.Render("—")
}

func (m *Model) refreshHistory() {
	rows := make([]table.Row, 0, len(m.changes))
	for _, c := range m.changes {
		rows = append(rows, table.Row{
			report.ChainName(c.Chain),
			c.Name,
			errStyle.Render(money(c.OldPrice)),
			okStyle.Render(money(c.NewPrice)),
			dimStyle.Render(c.FetchedAt.Format(time.DateTime)),
		})
	}
	m.table.SetHeight(clamp(m.height-10, 5, 55))
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
