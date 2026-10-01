// Package report genera los informes markdown: uno por supermercado con su
// lista de compra y otro final con la opción más barata de cada producto.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/miferdev/superComparator/internal/core"
)

// Generate construye el informe final: la opción más barata de cada producto
// con su enlace, los totales por supermercado y los avisos.
func Generate(cmp core.Comparison) string {
	var b strings.Builder
	b.WriteString("# Comparativa de la compra\n\n")
	fmt.Fprintf(&b, "_Generado el %s_\n\n", cmp.GeneratedAt.Format("02/01/2006 15:04"))
	fmt.Fprintf(&b, "Compra mixta: **%s**", money(cmp.MixedTotal))
	if cmp.CheapestChain != "" {
		fmt.Fprintf(&b, " · la más barata es **%s** (%s)", ChainName(cmp.CheapestChain), money(cmp.Totals[cmp.CheapestChain]))
	}
	b.WriteString("\n\n## Resumen\n\n")
	b.WriteString("| Producto | Cant. | Precio más barato | Supermercado | Producto elegido | Enlace |\n")
	b.WriteString("| --- | ---: | ---: | --- | --- | --- |\n")

	for _, item := range cmp.Items {
		opt, ok := optionFor(item, item.Cheapest)
		if item.Cheapest == "" || !ok {
			fmt.Fprintf(&b, "| %s | %d | — | — | — | — |\n", cell(item.Name), item.Quantity)
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s |\n",
			cell(item.Name),
			item.Quantity,
			money(opt.Price),
			cell(CheapestLabel(item)),
			cell(opt.Product),
			link(opt.URL, "ficha"),
		)
	}

	b.WriteString("\n## Totales por supermercado\n\n")
	b.WriteString("| Supermercado | Total | Informe |\n| --- | ---: | --- |\n")
	for _, chainID := range cmp.Chains {
		fmt.Fprintf(&b, "| %s | %s | %s |\n",
			ChainName(chainID), money(cmp.Totals[chainID]), link(ChainFile(chainID), "ver su lista"))
	}
	fmt.Fprintf(&b, "| **Compra mixta** | **%s** | |\n", money(cmp.MixedTotal))
	b.WriteString("\n")
	if cmp.CheapestChain != "" && cmp.MaxSaving > 0 {
		fmt.Fprintf(&b, "Comprando todo en %s se ahorran **%s** respecto a la más cara.\n",
			ChainName(cmp.CheapestChain), money(cmp.MaxSaving))
	}

	b.WriteString(changesSection(cmp))
	b.WriteString(reviewSection(cmp))
	return b.String()
}

// WriteAll escribe el informe de cada cadena y el final, y devuelve las rutas
// de todos los ficheros generados.
func WriteAll(path string, cmp core.Comparison) ([]string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for _, chainID := range cmp.Chains {
		chainPath := filepath.Join(dir, ChainFile(chainID))
		if err := write(chainPath, chainReport(cmp, chainID, filepath.Base(path))); err != nil {
			return written, err
		}
		written = append(written, chainPath)
	}
	if err := write(path, Generate(cmp)); err != nil {
		return written, err
	}
	return append(written, path), nil
}

func write(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
