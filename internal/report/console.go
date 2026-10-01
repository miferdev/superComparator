package report

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/miferdev/superComparator/internal/core"
)

// Console genera la comparativa en texto plano para la terminal: una columna
// por supermercado con la opción más barata de cada producto pedido.
func Console(cmp core.Comparison) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprint(w, "Producto\tCant.")
	for _, chainID := range cmp.Chains {
		fmt.Fprintf(w, "\t%s", ChainName(chainID))
	}
	fmt.Fprintln(w, "\tMás barato")
	for _, item := range cmp.Items {
		fmt.Fprintf(w, "%s\t%d", item.Name, item.Quantity)
		for _, chainID := range cmp.Chains {
			opt, ok := optionFor(item, chainID)
			if !ok {
				fmt.Fprint(w, "\t—")
				continue
			}
			cell := opt.Product
			if opt.MeasurePrice > 0 {
				cell += fmt.Sprintf(" (%s · %s)", money(opt.Price), unitPrice(opt.MeasurePrice, opt.MeasureUnit))
			} else {
				cell += fmt.Sprintf(" (%s)", money(opt.Price))
			}
			if item.Quantity > 1 {
				cell += " · " + money(opt.Price*float64(item.Quantity))
			}
			if opt.Promo {
				cell += " · oferta"
			}
			if !opt.Available {
				cell += " · no disponible"
			}
			if opt.Score > 0 && opt.Score < 0.5 {
				cell += " · revisar"
			}
			fmt.Fprintf(w, "\t%s", cell)
		}
		if item.Cheapest != "" {
			fmt.Fprintf(w, "\t%s", CheapestLabel(item))
		} else {
			fmt.Fprint(w, "\t—")
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	b.WriteString("\n")
	for _, chainID := range cmp.Chains {
		fmt.Fprintf(&b, "%-12s %8s\n", ChainName(chainID), money(cmp.Totals[chainID]))
	}
	fmt.Fprintf(&b, "%-12s %8s\n", "Mixta", money(cmp.MixedTotal))
	if cmp.CheapestChain != "" {
		fmt.Fprintf(&b, "Más barata: %s (ahorro %s)\n", ChainName(cmp.CheapestChain), money(cmp.MaxSaving))
	}
	if n := len(cmp.Review); n > 0 {
		fmt.Fprintf(&b, "Revisar: %d coincidencia(s) dudosa(s), mira la sección «Revisar» del informe\n", n)
	}
	return b.String()
}
