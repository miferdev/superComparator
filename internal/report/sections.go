package report

import (
	"fmt"
	"strings"

	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/match"
)

// changesSection lista los cambios de precio registrados en las ejecuciones
// anteriores. Solo aparece si ha habido alguno.
func changesSection(cmp core.Comparison) string {
	if len(cmp.Changes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Cambios de precio\n\n")
	b.WriteString("| Supermercado | Producto | Antes | Ahora | Enlace |\n")
	b.WriteString("| --- | --- | ---: | ---: | --- |\n")
	for _, c := range cmp.Changes {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			ChainName(c.Chain), cell(c.Name), money(c.OldPrice), money(c.NewPrice), link(c.URL, "ficha"))
	}
	return b.String()
}

// reviewSection avisa de los productos que quedaron con una coincidencia
// dudosa y propone alternativas. Solo aparece si hay alguno.
func reviewSection(cmp core.Comparison) string {
	if len(cmp.Review) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Revisar\n\n")
	fmt.Fprintf(&b, "Estas coincidencias están por debajo de %s de confianza: comprueba que el producto es el que buscabas.\n\n",
		decimal(match.AutoThreshold, 2))
	b.WriteString("| Producto | Supermercado | Confianza | Alternativas |\n")
	b.WriteString("| --- | --- | ---: | --- |\n")
	for _, r := range cmp.Review {
		var alts []string
		for _, alt := range r.Alternatives {
			entry := cell(alt.Name)
			if alt.Price > 0 {
				entry += " " + money(alt.Price)
			}
			alts = append(alts, link(alt.URL, entry))
		}
		if len(alts) == 0 {
			alts = append(alts, "sin alternativas")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			cell(r.Name), ChainName(r.Chain), decimal(r.Score, 2), strings.Join(alts, "<br>"))
	}
	return b.String()
}
