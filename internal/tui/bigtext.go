package tui

import (
	"fmt"
	"strings"
)

// bigGlyphs dibuja dígitos y separadores en bloques de 3x5 para destacar el
// total de la compra sin depender de la fuente de la terminal.
var bigGlyphs = map[rune][]string{
	'0': {"███", "█ █", "█ █", "█ █", "███"},
	'1': {" █ ", "██ ", " █ ", " █ ", "███"},
	'2': {"███", "  █", "███", "█  ", "███"},
	'3': {"███", "  █", "███", "  █", "███"},
	'4': {"█ █", "█ █", "███", "  █", "  █"},
	'5': {"███", "█  ", "███", "  █", "███"},
	'6': {"███", "█  ", "███", "█ █", "███"},
	'7': {"███", "  █", "  █", "  █", "  █"},
	'8': {"███", "█ █", "███", "█ █", "███"},
	'9': {"███", "█ █", "███", "  █", "███"},
	',': {"   ", "   ", "   ", " █ ", "█  "},
	'.': {"   ", "   ", "   ", "   ", " █ "},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// bigText convierte un texto en 5 filas de bloques.
func bigText(s string) []string {
	rows := make([]string, 5)
	for _, r := range s {
		glyph, ok := bigGlyphs[r]
		if !ok {
			continue
		}
		for i := range rows {
			rows[i] += glyph[i] + " "
		}
	}
	return rows
}

// totalIndent separa el total del borde izquierdo para que se lea bien.
const totalIndent = "  "

// bigMoney pinta un importe en dígitos grandes con el símbolo € en el centro.
func bigMoney(v float64) string {
	amount := strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
	rows := bigText(amount)
	for i := range rows {
		rows[i] = totalIndent + rows[i]
	}
	rows[2] += " €"
	return okStyle.Render(strings.Join(rows, "\n"))
}
