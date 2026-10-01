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
	for _, cov := range cmp.Coverage {
		marca := ""
		if !cov.Complete {
			marca = "  (parcial, no cubre la lista)"
		}
		fmt.Fprintf(&b, "%-12s %5d/%d  %8s%s\n", ChainName(cov.Chain), cov.Found, cov.Total, money(cmp.Totals[cov.Chain]), marca)
	}
	if cmp.MixedComplete() {
		fmt.Fprintf(&b, "%-12s %5d/%d  %8s  (%d tienda%s)\n", "Mixta",
			len(cmp.Items)-len(cmp.Missing), len(cmp.Items), money(cmp.MixedTotal), cmp.MixedStores, plural(cmp.MixedStores))
	}
	b.WriteString("\n")
	switch {
	case cmp.CheapestChain != "" && cmp.MixedComplete() && cmp.MixedTotal < cmp.Totals[cmp.CheapestChain]:
		fmt.Fprintf(&b, "Lista completa: compra mixta (%s) o %s en una sola tienda (%s)\n",
			money(cmp.MixedTotal), ChainName(cmp.CheapestChain), money(cmp.Totals[cmp.CheapestChain]))
	case cmp.CheapestChain != "":
		fmt.Fprintf(&b, "Lista completa en una sola tienda: %s (%s)", ChainName(cmp.CheapestChain), money(cmp.Totals[cmp.CheapestChain]))
		if cmp.MaxSaving > 0 {
			fmt.Fprintf(&b, ", ahorro %s", money(cmp.MaxSaving))
		}
		b.WriteString("\n")
	case cmp.MixedComplete():
		fmt.Fprintf(&b, "Ninguna cadena tiene la lista completa; la mixta cuesta %s\n", money(cmp.MixedTotal))
	default:
		b.WriteString("No se puede comprar la lista completa: hay productos sin ninguna cadena\n")
	}
	if n := len(cmp.Missing); n > 0 {
		fmt.Fprintf(&b, "Sin comprar: %s\n", strings.Join(cmp.Missing, ", "))
	}
	if n := len(cmp.Review); n > 0 {
		fmt.Fprintf(&b, "Revisar: %d coincidencia(s) dudosa(s), mira la sección «Revisar» del informe\n", n)
	}
	return b.String()
}
