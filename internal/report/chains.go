package report

import (
	"fmt"
	"strings"

	"github.com/miferdev/superComparator/internal/core"
)

// chainReport genera el informe de una cadena: la lista de la compra tal cual
// está en lista.md, con el producto encontrado y su precio en cada casa.
// finalBase es el nombre del informe comparativo, al que se vuelve con un enlace.
func chainReport(cmp core.Comparison, chainID, finalBase string) string {
	name := ChainName(chainID)
	cov := cmp.CoverageOf(chainID)
	var b strings.Builder
	fmt.Fprintf(&b, "# Compra en %s\n\n", name)
	b.WriteString(generatedAt(cmp.GeneratedAt))
	fmt.Fprintf(&b, "Esta cadena tiene **%d de %d** productos de tu lista", cov.Found, cov.Total)
	if !cov.Complete {
		fmt.Fprintf(&b, ": no sirve para hacer la compra entera, le faltan %d", cov.Total-cov.Found)
	}
	b.WriteString(".\n\n")

	b.WriteString("| Producto | Cantidad | Producto encontrado | Unidad | €/kg o €/L | Oferta | Enlace |\n")
	b.WriteString("| --- | ---: | --- | ---: | ---: | --- | --- |\n")
	for _, item := range cmp.Items {
		fmt.Fprintf(&b, "| %s | %d |", cell(item.Name), item.Quantity)
		opt, ok := optionFor(item, chainID)
		if !ok {
			b.WriteString(" no encontrado | — | — | — | — |\n")
			continue
		}
		fmt.Fprintf(&b, " %s | %s | %s | %s | %s |\n",
			cell(opt.Product),
			money(opt.Price),
			unitPrice(opt.MeasurePrice, opt.MeasureUnit),
			promoText(opt),
			link(opt.URL, "ficha"),
		)
	}

	if len(cov.Missing) > 0 {
		b.WriteString("\n## Le faltan\n\n")
		for _, name := range cov.Missing {
			fmt.Fprintf(&b, "- %s\n", cell(name))
		}
	}

	fmt.Fprintf(&b, "\n**Total de lo que sí tiene: %s**", money(cmp.Totals[chainID]))
	if !cov.Complete {
		b.WriteString(" _(total parcial: no incluye los productos que faltan)_")
	}
	b.WriteString("\n")
	if cmp.CheapestChain == chainID {
		fmt.Fprintf(&b, "\nEs la más barata de las que tienen la lista completa: %s.\n", money(cmp.Totals[chainID]))
	}
	if len(reviewOf(cmp, chainID)) > 0 {
		b.WriteString("\n## Revisar\n\n")
		b.WriteString("En estas líneas la coincidencia no es concluyente:\n\n")
		for _, r := range reviewOf(cmp, chainID) {
			fmt.Fprintf(&b, "- **%s** (confianza %s)\n", cell(r.Name), decimal(r.Score, 2))
			for _, alt := range r.Alternatives {
				fmt.Fprintf(&b, "  - %s — %s\n", cell(alt.Name), link(alt.URL, "ficha"))
			}
		}
	}
	fmt.Fprintf(&b, "\n[Volver a la comparativa](%s)\n", finalBase)
	return b.String()
}

// promoText describe la oferta de la opción, o su ausencia.
func promoText(opt core.ChainOption) string {
	switch {
	case !opt.Promo:
		return "—"
	case opt.OldPrice > 0:
		return fmt.Sprintf("%s (antes %s)", money(opt.Price), money(opt.OldPrice))
	default:
		return "oferta"
	}
}

// reviewOf filtra los avisos de revisión de una sola cadena.
func reviewOf(cmp core.Comparison, chainID string) []core.ReviewItem {
	var out []core.ReviewItem
	for _, r := range cmp.Review {
		if r.Chain == chainID {
			out = append(out, r)
		}
	}
	return out
}
