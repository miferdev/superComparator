package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/store"
)

func ejemplo() core.Comparison {
	return core.Comparison{
		Chains: []string{"mercadona", "ahorramas"},
		Totals: map[string]float64{"mercadona": 7.35, "ahorramas": 6.90},
		Items: []core.ItemComparison{{
			Name: "Leche entera 1L", Quantity: 3, Cheapest: "ahorramas", Save: 0.45, Criterion: "€/l",
			Options: []core.ChainOption{
				{Chain: "mercadona", Product: "Leche entera Hacendado", URL: "https://m/1", Price: 2.45, MeasurePrice: 2.45, MeasureUnit: "l", Score: 0.9},
				{Chain: "ahorramas", Product: "Leche entera Alipende", URL: "https://a/1", Price: 2.30, MeasurePrice: 2.30, MeasureUnit: "l", OldPrice: 2.80, Promo: true, Score: 0.8},
			},
		}},
		MixedTotal:    6.90,
		CheapestChain: "ahorramas",
		MaxSaving:     0.45,
		GeneratedAt:   time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
}

func TestGenerate(t *testing.T) {
	out := Generate(ejemplo())
	for _, want := range []string{
		"# Comparativa de la compra",
		"Mercadona", "Ahorramas",
		"7,35 €", "6,90 €",
		"Leche entera Alipende",
		"[ficha](https://a/1)",
		"Ahorramas (€/l)",
		"ver su lista",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("el informe no contiene %q:\n%s", want, out)
		}
	}
}

func TestGenerateSinGanador(t *testing.T) {
	cmp := ejemplo()
	cmp.Items[0].Cheapest = ""
	out := Generate(cmp)
	if !strings.Contains(out, "| Leche entera 1L | 3 | — | — | — | — |") {
		t.Errorf("debería marcar el producto como no resuelto:\n%s", out)
	}
}

func TestInformePorCadena(t *testing.T) {
	out := chainReport(ejemplo(), "mercadona", "informe.md")
	for _, want := range []string{
		"# Compra en Mercadona",
		"| Producto | Cantidad | Producto encontrado |",
		"Leche entera Hacendado",
		"2,45 €",
		"2,45 €/l",
		"[ficha](https://m/1)",
		"**Total en Mercadona: 7,35 €**",
		"[Volver a la comparativa](informe.md)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("el informe de Mercadona no contiene %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Alipende") {
		t.Error("el informe de Mercadona no debe mencionar productos de otra cadena")
	}
}

func TestInformePorCadenaProductoNoEncontrado(t *testing.T) {
	cmp := ejemplo()
	cmp.Items = append(cmp.Items, core.ItemComparison{
		Name: "Kéfir", Quantity: 1,
		Options: []core.ChainOption{{Chain: "mercadona", Product: "Kéfir X", URL: "https://m/2", Price: 1.10, Score: 0.7}},
	})
	out := chainReport(cmp, "ahorramas", "informe.md")
	if !strings.Contains(out, "| Kéfir | 1 | no encontrado | — | — | — | — |") {
		t.Errorf("debería indicar que no está en Ahorramas:\n%s", out)
	}
}

func TestSeccionRevisar(t *testing.T) {
	cmp := ejemplo()
	cmp.Review = []core.ReviewItem{{
		Name: "Leche entera 1L", Chain: "mercadona", Score: 0.31,
		Alternatives: []chain.Alternative{{URL: "https://m/2", Name: "Leche de soja", Score: 0.29, Price: 1.20}},
	}}
	out := Generate(cmp)
	if !strings.Contains(out, "## Revisar") || !strings.Contains(out, "0,31") {
		t.Errorf("debería incluir la sección de revisión:\n%s", out)
	}
}

func TestSeccionCambios(t *testing.T) {
	cmp := ejemplo()
	out := Generate(cmp)
	if strings.Contains(out, "## Cambios de precio") {
		t.Errorf("sin cambios no debe aparecer la sección:\n%s", out)
	}
	cmp.Changes = []store.Change{{Chain: "ahorramas", Name: "Leche entera Alipende", URL: "https://a/1", OldPrice: 2.80, NewPrice: 2.30}}
	out = Generate(cmp)
	for _, want := range []string{"## Cambios de precio", "2,80 €", "2,30 €"} {
		if !strings.Contains(out, want) {
			t.Errorf("la sección de cambios no contiene %q:\n%s", want, out)
		}
	}
}

func TestWriteAll(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "informe.md")
	written, err := WriteAll(final, ejemplo())
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 3 {
		t.Fatalf("se esperaban 3 ficheros, se escribieron %d: %v", len(written), written)
	}
	for _, path := range written {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Errorf("%s está vacío", path)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ChainFile("ahorramas"))); err != nil {
		t.Errorf("falta el informe de Ahorramas: %v", err)
	}
}

func TestConsole(t *testing.T) {
	console := Console(ejemplo())
	for _, want := range []string{"Producto", "Cant.", "Mercadona", "Ahorramas", "Más barato", "2,30", "6,90 €", "Mixta", "oferta"} {
		if !strings.Contains(console, want) {
			t.Errorf("la salida de consola no contiene %q:\n%s", want, console)
		}
	}
}

func TestConsoleMarcaRevisar(t *testing.T) {
	cmp := ejemplo()
	cmp.Items[0].Options[1].Score = 0.2
	if !strings.Contains(Console(cmp), "revisar") {
		t.Error("la consola debería avisar de la coincidencia dudosa")
	}
}
