package report

import (
	"strings"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/core"
)

func TestGenerate(t *testing.T) {
	cmp := core.Comparison{
		Chains: []string{"mercadona", "ahorramas"},
		Totals: map[string]float64{"mercadona": 7.35, "ahorramas": 6.90},
		Items: []core.ItemComparison{{
			Name: "Leche entera 1L", Quantity: 3, Cheapest: "ahorramas", Save: 0.45,
			Options: []core.ChainOption{
				{Chain: "mercadona", Product: "Leche entera Hacendado", Price: 2.45, MeasurePrice: 2.45, MeasureUnit: "l"},
				{Chain: "ahorramas", Product: "Leche entera Alipende", Price: 2.30, MeasurePrice: 2.30, MeasureUnit: "l", OldPrice: 2.80, Promo: true},
			},
		}},
		MixedTotal:    6.90,
		CheapestChain: "ahorramas",
		MaxSaving:     0.45,
		GeneratedAt:   time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
	out := Generate(cmp)
	for _, want := range []string{"# Informe de la compra", "Mercadona", "Ahorramas", "7,35 €", "6,90 €", "OFERTA", "Ahorro máximo"} {
		if !strings.Contains(out, want) {
			t.Errorf("el informe no contiene %q:\n%s", want, out)
		}
	}
}
