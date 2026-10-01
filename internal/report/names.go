package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/version"
)

// ChainName traduce el identificador de la cadena a su nombre comercial.
func ChainName(id string) string {
	switch id {
	case "mercadona":
		return "Mercadona"
	case "ahorramas":
		return "Ahorramas"
	case "dia":
		return "DÍA"
	case "alcampo":
		return "Alcampo"
	default:
		return id
	}
}

// ChainFile es el nombre del informe markdown propio de cada cadena.
func ChainFile(id string) string { return id + ".md" }

// optionFor devuelve la opción con precio de la cadena indicada.
func optionFor(item core.ItemComparison, chainID string) (core.ChainOption, bool) {
	for _, opt := range item.Options {
		if opt.Chain == chainID && opt.Price > 0 {
			return opt, true
		}
	}
	return core.ChainOption{}, false
}

// CheapestLabel añade el criterio usado (€/kg, €/l o total) al ganador del
// producto, para que se entienda por qué se eligió.
func CheapestLabel(item core.ItemComparison) string {
	label := ChainName(item.Cheapest)
	if item.Criterion != "" {
		label += " (" + item.Criterion + ")"
	}
	return label
}

// generatedAt es la línea de cabecera de los informes: cuándo se generó y con
// qué versión del programa.
func generatedAt(t time.Time) string {
	return fmt.Sprintf("_Generado el %s · %s_\n\n", t.Format("02/01/2006 15:04"), version.Stamp())
}

func money(v float64) string { return decimal(v, 2) + " €" }

// decimal formatea un número con coma decimal, como corresponde al español.
func decimal(v float64, decimals int) string {
	return strings.Replace(fmt.Sprintf("%.*f", decimals, v), ".", ",", 1)
}

// cell escapa los separadores de tabla markdown que puedan venir en un nombre.
func cell(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "|", "/")
}

// link formatea un enlace a la ficha del producto.
func link(url, text string) string {
	if url == "" {
		return "—"
	}
	return fmt.Sprintf("[%s](%s)", cell(text), url)
}

// unitPrice formatea el precio por unidad de medida, si la cadena lo publica.
func unitPrice(price float64, unit string) string {
	if price <= 0 || unit == "" {
		return "—"
	}
	return fmt.Sprintf("%s/%s", money(price), strings.ToLower(unit))
}
