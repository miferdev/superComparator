// Este test vive en el paquete externo porque comprueba el camino completo del
// catálogo: el nombre legible y el formato salen de chain, pero la medida la
// normaliza match, que importa a chain.
package chain_test

import (
	"math"
	"testing"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/match"
)

// TestMedidaDesdeSlug cubre los dos casos reales de catálogo que salen mal si
// el formato se confunde con la medida: un pack (2 x 500 ml son 1 l de
// producto, no 500 ml) y un peso pegado a su unidad detrás de un código que no
// es una medida.
func TestMedidaDesdeSlug(t *testing.T) {
	casos := []struct {
		slug   string
		format string
		value  float64
		unit   string
	}{
		{
			slug:   "kin-enjuague-bucal-diario-con-acción-antiplaca-kin-2-x-500-ml",
			format: "2 x 500 ml",
			value:  1,
			unit:   "l",
		},
		{
			slug:   "pan-de-trigo-de-espelta-64-400g",
			format: "400 g",
			value:  0.4,
			unit:   "kg",
		},
	}
	for _, c := range casos {
		nombre := chain.HumanizeSlug(c.slug)
		formato := chain.FormatFromName(nombre)
		if formato != c.format {
			t.Errorf("FormatFromName(%q) = %q, want %q", nombre, formato, c.format)
			continue
		}
		m, ok := match.ParseMeasure(formato)
		if !ok {
			t.Errorf("ParseMeasure(%q) no ha medido nada", formato)
			continue
		}
		if m.Unit != c.unit || math.Abs(m.Value-c.value) > 1e-9 {
			t.Errorf("medida de %q = %v %s, want %v %s", formato, m.Value, m.Unit, c.value, c.unit)
		}
	}
}
