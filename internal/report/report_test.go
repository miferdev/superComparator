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

// ejemplo construye una comparativa de dos productos que están en las dos
// cadenas, con la cobertura ya calculada.
func ejemplo() core.Comparison {
	cmp := core.Comparison{
		Chains: []string{"mercadona", "ahorramas"},
		Totals: map[string]float64{"mercadona": 8.15, "ahorramas": 8.80},
		Items: []core.ItemComparison{
			{
				Name: "Leche entera 1L", Quantity: 3, Cheapest: "ahorramas", Criterion: "€/l", Save: 0.45,
				Options: []core.ChainOption{
					{Chain: "mercadona", Product: "Leche entera Hacendado", URL: "https://m/1", Price: 2.45, MeasurePrice: 2.45, MeasureUnit: "l", Score: 0.9},
					{Chain: "ahorramas", Product: "Leche entera Alipende", URL: "https://a/1", Price: 2.60, MeasurePrice: 2.60, MeasureUnit: "l", OldPrice: 2.90, Promo: true, Score: 0.8},
				},
			},
			{
				Name: "Pan de molde", Quantity: 1, Cheapest: "mercadona", Criterion: "€/kg",
				Options: []core.ChainOption{
					{Chain: "mercadona", Product: "Pan de molde Hacendado", URL: "https://m/2", Price: 0.80, MeasurePrice: 1.74, MeasureUnit: "kg", Score: 0.9},
					{Chain: "ahorramas", Product: "Pan de molde Alipende", URL: "https://a/2", Price: 1.00, MeasurePrice: 2.33, MeasureUnit: "kg", Score: 0.8},
				},
			},
		},
		MixedTotal:    8.15,
		MixedStores:   1,
		CheapestChain: "mercadona",
		MaxSaving:     0.65,
		GeneratedAt:   time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
	cmp.FillCoverage()
	return cmp
}

func TestGenerate(t *testing.T) {
	out := Generate(ejemplo())
	for _, want := range []string{
		"# Comparativa de la compra",
		"Para comprar **toda** la lista",
		"Mercadona", "Ahorramas",
		"8,15 €", "8,80 €",
		"Pan de molde Hacendado",
		"[ficha](https://m/2)",
		"Mercadona (€/kg)",
		"ver su lista",
		"| Mercadona | 2 de 2 | 8,15 € |",
		"| Ahorramas | 2 de 2 | 8,80 € |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("el informe no contiene %q:\n%s", want, out)
		}
	}
}

// TestGenerateCadenaIncompletaEsElCasoClave: una cadena con la lista
// incompleta no puede ganar el título de más barata aunque su total sea menor.
func TestGenerateCadenaIncompleta(t *testing.T) {
	cmp := core.Comparison{
		Chains: []string{"mercadona", "dia"},
		Totals: map[string]float64{"mercadona": 10.00, "dia": 1.29},
		Items: []core.ItemComparison{
			{
				Name: "Leche 1L", Quantity: 1, Cheapest: "mercadona", Criterion: "total",
				Options: []core.ChainOption{{Chain: "mercadona", Product: "Leche Hacendado", URL: "https://m/1", Price: 10, Score: 0.9}},
			},
			{
				Name: "Pan de molde", Quantity: 1, Cheapest: "dia", Criterion: "total",
				Options: []core.ChainOption{{Chain: "dia", Product: "Pan DÍA", URL: "https://d/1", Price: 1.29, Score: 0.8}},
			},
		},
		MixedTotal:    11.29,
		MixedStores:   2,
		CheapestChain: "mercadona",
		GeneratedAt:   time.Now(),
	}
	cmp.FillCoverage()
	out := Generate(cmp)
	for _, want := range []string{
		"Para comprar **toda** la lista en una sola tienda: **Mercadona**, 10,00 €",
		"| DÍA | 1 de 2 _(parcial)_ | 1,29 € |",
		"no tiene la lista completa",
		"su total no sirve para hacer la compra entera",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("el informe no contiene %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "**DÍA**, 1,29 €") {
		t.Error("DÍA no debe aparecer como la más barata con la lista incompleta")
	}
}

func TestGenerateRecomiendaMixta(t *testing.T) {
	cmp := core.Comparison{
		Chains: []string{"mercadona", "ahorramas"},
		Totals: map[string]float64{"mercadona": 10.00, "ahorramas": 12.00},
		Items: []core.ItemComparison{
			{
				Name: "Leche 1L", Quantity: 1, Cheapest: "mercadona",
				Options: []core.ChainOption{
					{Chain: "mercadona", Product: "Leche M", URL: "https://m/1", Price: 5, Score: 0.9},
					{Chain: "ahorramas", Product: "Leche A", URL: "https://a/1", Price: 6, Score: 0.8},
				},
			},
			{
				Name: "Pan", Quantity: 1, Cheapest: "ahorramas",
				Options: []core.ChainOption{
					{Chain: "mercadona", Product: "Pan M", URL: "https://m/2", Price: 9, Score: 0.9},
					{Chain: "ahorramas", Product: "Pan A", URL: "https://a/2", Price: 3, Score: 0.8},
				},
			},
		},
		MixedTotal:    8,
		MixedStores:   2,
		CheapestChain: "mercadona",
		MaxSaving:     2,
		GeneratedAt:   time.Now(),
	}
	cmp.FillCoverage()
	out := Generate(cmp)
	if !strings.Contains(out, "Para comprar **toda** la lista: **compra mixta en Mercadona + Ahorramas**, 8,00 €") {
		t.Errorf("debería recomendar la compra mixta:\n%s", out)
	}
	if !strings.Contains(out, "la más barata con la lista completa es **Mercadona**") {
		t.Errorf("debería mencionar la alternativa en una sola tienda:\n%s", out)
	}
}

func TestGenerateSinComprar(t *testing.T) {
	cmp := core.Comparison{
		Chains:      []string{"mercadona"},
		Totals:      map[string]float64{"mercadona": 5},
		Items:       []core.ItemComparison{{Name: "Leche 1L", Quantity: 1, Cheapest: "mercadona", Options: []core.ChainOption{{Chain: "mercadona", Product: "Leche M", URL: "https://m/1", Price: 5, Score: 0.9}}}},
		Missing:     []string{"Kéfir"},
		MixedTotal:  5,
		MixedStores: 1,
		GeneratedAt: time.Now(),
	}
	cmp.FillCoverage()
	out := Generate(cmp)
	if !strings.Contains(out, "## Sin comprar") || !strings.Contains(out, "Kéfir") {
		t.Errorf("debería tener la sección Sin comprar:\n%s", out)
	}
	if strings.Contains(out, "Compra mixta") {
		t.Error("una compra mixta incompleta no debe presentarse como opción")
	}
}

func TestInformePorCadena(t *testing.T) {
	out := chainReport(ejemplo(), "mercadona", "informe.md")
	for _, want := range []string{
		"# Compra en Mercadona",
		"Esta cadena tiene **2 de 2** productos",
		"| Producto | Cantidad | Producto encontrado |",
		"Pan de molde Hacendado",
		"0,80 €",
		"1,74 €/kg",
		"[ficha](https://m/2)",
		"**Total de lo que sí tiene: 8,15 €**",
		"más barata de las que tienen la lista completa",
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

func TestInformePorCadenaIncompleta(t *testing.T) {
	cmp := core.Comparison{
		Chains: []string{"dia"},
		Totals: map[string]float64{"dia": 1.29},
		Items: []core.ItemComparison{
			{Name: "Pan de molde", Quantity: 1, Cheapest: "dia", Options: []core.ChainOption{{Chain: "dia", Product: "Pan DÍA", URL: "https://d/1", Price: 1.29, Score: 0.8}}},
			{Name: "Leche 1L", Quantity: 3},
		},
		MixedTotal: 1.29, MixedStores: 1, GeneratedAt: time.Now(),
	}
	cmp.FillCoverage()
	out := chainReport(cmp, "dia", "informe.md")
	for _, want := range []string{
		"Esta cadena tiene **1 de 2** productos",
		"no sirve para hacer la compra entera",
		"## Le faltan",
		"- Leche 1L",
		"total parcial",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("el informe de DÍA no contiene %q:\n%s", want, out)
		}
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
	if strings.Contains(Generate(cmp), "## Cambios de precio") {
		t.Error("sin cambios no debe aparecer la sección")
	}
	cmp.Changes = []store.Change{{Chain: "ahorramas", Name: "Leche entera Alipende", URL: "https://a/1", OldPrice: 2.80, NewPrice: 2.30}}
	out := Generate(cmp)
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
	for _, want := range []string{
		"Producto", "Cant.", "Mercadona", "Ahorramas", "Más barato",
		"2/2", "Lista completa en una sola tienda: Mercadona (8,15 €)",
		"Mixta",
	} {
		if !strings.Contains(console, want) {
			t.Errorf("la salida de consola no contiene %q:\n%s", want, console)
		}
	}
}

func TestConsoleCadenaParcial(t *testing.T) {
	cmp := core.Comparison{
		Chains: []string{"mercadona", "dia"},
		Totals: map[string]float64{"mercadona": 10, "dia": 1.29},
		Items: []core.ItemComparison{
			{Name: "Leche", Quantity: 1, Cheapest: "mercadona", Options: []core.ChainOption{{Chain: "mercadona", Product: "Leche M", URL: "https://m/1", Price: 10, Score: 0.9}}},
			{Name: "Pan", Quantity: 1, Cheapest: "dia", Options: []core.ChainOption{{Chain: "dia", Product: "Pan D", URL: "https://d/1", Price: 1.29, Score: 0.8}}},
		},
		MixedTotal: 11.29, MixedStores: 2, CheapestChain: "mercadona", GeneratedAt: time.Now(),
	}
	cmp.FillCoverage()
	console := Console(cmp)
	for _, want := range []string{
		"DÍA", "1/2", "parcial, no cubre la lista",
		"Lista completa en una sola tienda: Mercadona (10,00 €)",
	} {
		if !strings.Contains(console, want) {
			t.Errorf("la consola no contiene %q:\n%s", want, console)
		}
	}
	if strings.Contains(console, "Más barata: DÍA") {
		t.Error("no debe dar la más barata a una cadena incompleta")
	}
}

func TestConsoleMarcaRevisar(t *testing.T) {
	cmp := ejemplo()
	cmp.Items[0].Options[1].Score = 0.2
	if !strings.Contains(Console(cmp), "revisar") {
		t.Error("la consola debería avisar de la coincidencia dudosa")
	}
}
