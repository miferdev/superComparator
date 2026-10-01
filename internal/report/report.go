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
	b.WriteString(recommendation(cmp))
	b.WriteString("\n## Resumen\n\n")
	b.WriteString("| Producto | Cant. | Precio más barato | Supermercado | Producto elegido | Enlace |\n")
	b.WriteString("| --- | ---: | ---: | --- | --- | --- |\n")

	for _, item := range cmp.Items {
		opt, ok := optionFor(item, item.Cheapest)
		if item.Cheapest == "" || !ok {
			fmt.Fprintf(&b, "| %s | %d | — | %s | — | — |\n",
				cell(item.Name), item.Quantity, missingLabel(cmp, item.Name))
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
	b.WriteString("| Supermercado | Productos | Total | Informe |\n")
	b.WriteString("| --- | ---: | ---: | --- |\n")
	for _, cov := range cmp.Coverage {
		nota := ""
		if !cov.Complete {
			nota = " _(parcial)_"
		}
		fmt.Fprintf(&b, "| %s | %d de %d%s | %s | %s |\n",
			ChainName(cov.Chain), cov.Found, cov.Total, nota, money(cmp.Totals[cov.Chain]), link(ChainFile(cov.Chain), "ver su lista"))
	}
	if cmp.MixedComplete() {
		fmt.Fprintf(&b, "| **Compra mixta** (%d tienda%s) | %d de %d | **%s** | |\n",
			cmp.MixedStores, plural(cmp.MixedStores), len(cmp.Items)-len(cmp.Missing), len(cmp.Items), money(cmp.MixedTotal))
	}
	b.WriteString("\n")
	b.WriteString(partialsNote(cmp))
	b.WriteString(missingSection(cmp))
	b.WriteString(changesSection(cmp))
	b.WriteString(reviewSection(cmp))
	return b.String()
}

// recommendation dice, en la primera línea, cómo comprar la lista entera. Solo
// cuentan las cadenas con todos los productos: una cadena incompleta no sirve,
// aunque su total parcial parezca el más bajo.
func recommendation(cmp core.Comparison) string {
	var b strings.Builder
	switch {
	case cmp.CheapestChain != "" && cmp.MixedComplete() && cmp.MixedTotal < cmp.Totals[cmp.CheapestChain]:
		fmt.Fprintf(&b, "Para comprar **toda** la lista: **compra mixta en %s**, %s repartidos en %d tienda%s.",
			mixedChainNames(cmp), money(cmp.MixedTotal), cmp.MixedStores, plural(cmp.MixedStores))
		if cmp.MaxSaving > 0 {
			fmt.Fprintf(&b, " En una sola tienda, la más barata con la lista completa es **%s** (%s).",
				ChainName(cmp.CheapestChain), money(cmp.Totals[cmp.CheapestChain]))
		}
	case cmp.CheapestChain != "":
		fmt.Fprintf(&b, "Para comprar **toda** la lista en una sola tienda: **%s**, %s.",
			ChainName(cmp.CheapestChain), money(cmp.Totals[cmp.CheapestChain]))
		if cmp.MaxSaving > 0 {
			fmt.Fprintf(&b, " Ahorras %s frente a la más cara con la lista completa.", money(cmp.MaxSaving))
		}
	case cmp.MixedComplete():
		fmt.Fprintf(&b, "Ninguna cadena tiene la lista completa. La compra mixta más barata cuesta **%s** en %d tienda%s.",
			money(cmp.MixedTotal), cmp.MixedStores, plural(cmp.MixedStores))
	default:
		b.WriteString("No hay forma de comprar la lista completa: hay productos que ninguna cadena tiene.")
	}
	b.WriteString("\n\n")
	return b.String()
}

// partialsNote explica los totales parciales, para que no se confundan con el
// precio de la compra entera.
func partialsNote(cmp core.Comparison) string {
	var incompletas []string
	for _, cov := range cmp.Coverage {
		if !cov.Complete && cov.Found > 0 {
			incompletas = append(incompletas, fmt.Sprintf("**%s** (%d de %d)", ChainName(cov.Chain), cov.Found, cov.Total))
		}
	}
	if len(incompletas) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s no tiene la lista completa, así que su total no sirve para hacer la compra entera.\n\n",
		strings.Join(incompletas, ", "))
	b.WriteString("En el informe de cada cadena tienes qué productos le faltan y cuáles encontró.\n\n")
	return b.String()
}

// missingSection lista los productos que no se pueden comprar en ninguna
// cadena activa, que es lo que impide cerrar la lista.
func missingSection(cmp core.Comparison) string {
	if len(cmp.Missing) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Sin comprar\n\n")
	fmt.Fprintf(&b, "Estos %d producto%s no están en ninguna de las cadenas comparadas:\n\n", len(cmp.Missing), plural(len(cmp.Missing)))
	for _, name := range cmp.Missing {
		fmt.Fprintf(&b, "- %s\n", cell(name))
	}
	b.WriteString("\n")
	return b.String()
}

// missingLabel explica por qué un producto no tiene precio en la comparativa.
func missingLabel(cmp core.Comparison, itemName string) string {
	for _, name := range cmp.Missing {
		if name == itemName {
			return "no está en ninguna cadena"
		}
	}
	return "—"
}

// mixedChainNames devuelve los nombres de las cadenas donde se compra cada
// producto de la compra mixta, sin repetir.
func mixedChainNames(cmp core.Comparison) string {
	var names []string
	for _, item := range cmp.Items {
		if item.Cheapest != "" {
			names = append(names, ChainName(item.Cheapest))
		}
	}
	return strings.Join(uniq(names), " + ")
}

func uniq(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
