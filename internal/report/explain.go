package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/core"
)

// Explain genera la explicación en markdown: para cada producto de la lista,
// qué fichas se encontraron en cada cadena, con su puntuación y el motivo por
// el que se aceptaron o se descartaron.
func Explain(explicaciones []core.Explicacion) string {
	var b strings.Builder
	b.WriteString("# Por qué se eligió cada producto\n\n")
	b.WriteString(generatedAt(time.Now()))
	b.WriteString("Puntuación sobre 1: por encima de 0,55 se acepta el producto. ")
	b.WriteString("«Elegido» marca el que entra en el informe.\n\n")
	for _, exp := range explicaciones {
		fmt.Fprintf(&b, "## %s\n\n", cell(exp.Item))
		for _, cadena := range exp.Cadenas {
			fmt.Fprintf(&b, "### %s\n\n", ChainName(cadena.Chain))
			if len(cadena.Candidatos) == 0 {
				fmt.Fprintf(&b, "- %s\n\n", cell(cadena.Nota))
				continue
			}
			b.WriteString("| Producto encontrado | Precio | Puntuación | Veredicto | Enlace |\n")
			b.WriteString("| --- | ---: | ---: | --- | --- |\n")
			for _, cand := range cadena.Candidatos {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
					cell(cand.Nombre), money(cand.Precio), decimal(cand.Score, 2),
					verdictText(cand), link(cand.URL, "ficha"))
			}
			b.WriteString("\n")
			if cadena.Nota != "" {
				fmt.Fprintf(&b, "> %s\n\n", cell(cadena.Nota))
			}
		}
	}
	return b.String()
}

func verdictText(cand core.CandidatoExplicado) string {
	switch {
	case cand.Elegido:
		return "**elegido**"
	case cand.Aceptado:
		return "aceptado, pero más caro"
	case len(cand.Motivos) > 0:
		return "descartado: " + strings.Join(cand.Motivos, "; ")
	default:
		return "descartado: cobertura baja"
	}
}

// WriteExplain deja la explicación junto a los informes.
func WriteExplain(path string, explicaciones []core.Explicacion) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Explain(explicaciones)), 0o644)
}

// ExplainConsole muestra lo mismo en texto plano, para verlo en la terminal.
func ExplainConsole(explicaciones []core.Explicacion) string {
	var b strings.Builder
	for _, exp := range explicaciones {
		fmt.Fprintf(&b, "\n%s\n", exp.Item)
		for _, cadena := range exp.Cadenas {
			fmt.Fprintf(&b, "  %s\n", ChainName(cadena.Chain))
			if len(cadena.Candidatos) == 0 {
				fmt.Fprintf(&b, "    %s\n", cadena.Nota)
				continue
			}
			for _, cand := range cadena.Candidatos {
				fmt.Fprintf(&b, "    %s %-7s %-34s %s\n",
					decimal(cand.Score, 2), money(cand.Precio), verdictText(cand), cell(cand.Nombre))
			}
		}
	}
	return b.String()
}
