package chain

import "strings"

// CategoryPath une los niveles de una categoría en el formato de
// Product.Category: la ruta completa, del nivel superior al más fino, con los
// niveles separados por " / ". Los niveles vacíos se descartan, para que una
// miga de pan a medias no deje un separador suelto.
func CategoryPath(levels ...string) string {
	limpios := make([]string, 0, len(levels))
	for _, nivel := range levels {
		nivel = strings.Join(strings.Fields(nivel), " ")
		if nivel != "" {
			limpios = append(limpios, nivel)
		}
	}
	return strings.Join(limpios, " / ")
}
