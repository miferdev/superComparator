// Package report genera el informe markdown de la comparativa.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/miferdev/superComparator/internal/core"
)

func Generate(cmp core.Comparison) string {
	var b strings.Builder
	b.WriteString("# Informe de la compra\n\n")
	fmt.Fprintf(&b, "_Generado el %s_\n\n", cmp.GeneratedAt.Format("02/01/2006 15:04"))

	b.WriteString("| Producto | Cant. |")
	for _, chainID := range cmp.Chains {
		fmt.Fprintf(&b, " %s |", ChainName(chainID))
	}
	b.WriteString(" Más barato |\n")
	b.WriteString("| --- | ---: |")
	for range cmp.Chains {
		b.WriteString(" ---: |")
	}
	b.WriteString(" --- |\n")

	for _, item := range cmp.Items {
		fmt.Fprintf(&b, "| %s | %d |", item.Name, item.Quantity)
		for _, chainID := range cmp.Chains {
			opt, ok := optionFor(item, chainID)
			if !ok || opt.Price <= 0 {
				b.WriteString(" — |")
				continue
			}
			cell := fmt.Sprintf("%s — %s", opt.Product, money(opt.Price))
			if item.Quantity > 1 {
				cell += fmt.Sprintf(" (%s)", money(opt.Price*float64(item.Quantity)))
			}
			if opt.MeasurePrice > 0 {
				cell += fmt.Sprintf(" · %s/%s", money(opt.MeasurePrice), opt.MeasureUnit)
			}
			if opt.Promo {
				cell += " · OFERTA"
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		if item.Cheapest != "" {
			fmt.Fprintf(&b, " %s |\n", ChainName(item.Cheapest))
		} else {
			b.WriteString(" — |\n")
		}
	}

	b.WriteString("\n## Totales\n\n")
	b.WriteString("| Cadena | Total |\n| --- | ---: |\n")
	for _, chainID := range cmp.Chains {
		fmt.Fprintf(&b, "| %s | %s |\n", ChainName(chainID), money(cmp.Totals[chainID]))
	}
	fmt.Fprintf(&b, "| Compra mixta | %s |\n", money(cmp.MixedTotal))

	b.WriteString("\n")
	if cmp.CheapestChain != "" {
		fmt.Fprintf(&b, "- Cadena más barata: **%s** (%s)\n", ChainName(cmp.CheapestChain), money(cmp.Totals[cmp.CheapestChain]))
		if cmp.MaxSaving > 0 {
			fmt.Fprintf(&b, "- Ahorro máximo comprando todo en la más barata: **%s**\n", money(cmp.MaxSaving))
		}
	}
	fmt.Fprintf(&b, "- Comprar lo más barato de cada casa: **%s**\n", money(cmp.MixedTotal))
	return b.String()
}

func Write(path string, cmp core.Comparison) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Generate(cmp)), 0o644)
}

func optionFor(item core.ItemComparison, chainID string) (core.ChainOption, bool) {
	for _, opt := range item.Options {
		if opt.Chain == chainID && opt.Price > 0 {
			return opt, true
		}
	}
	return core.ChainOption{}, false
}

func ChainName(id string) string {
	switch id {
	case "mercadona":
		return "Mercadona"
	case "ahorramas":
		return "Ahorramas"
	default:
		return id
	}
}

func money(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	return strings.Replace(s, ".", ",", 1) + " €"
}
