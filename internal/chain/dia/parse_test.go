package dia

import (
	"os"
	"testing"

	"github.com/miferdev/superComparator/internal/chain"
)

func TestParseProduct(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/dia_product.html")
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://www.dia.es/huevos-leche-y-mantequilla/leche/p/16065"
	p, err := ParseProduct(string(body), url)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Leche entera Asturiana 1 L" {
		t.Errorf("nombre = %q", p.Name)
	}
	if p.Price != 1.24 {
		t.Errorf("precio = %v, want 1.24", p.Price)
	}
	if p.MeasurePrice != 1.24 || p.MeasureUnit != "l" {
		t.Errorf("precio por unidad = %v %q, want 1.24 l", p.MeasurePrice, p.MeasureUnit)
	}
	if !p.Available {
		t.Error("el producto está InStock y debería estar disponible")
	}
	if p.Chain != "dia" || p.URL != url {
		t.Errorf("cadena/URL = %q/%q", p.Chain, p.URL)
	}
	if p.SKU != "16065" {
		t.Errorf("SKU = %q, want 16065", p.SKU)
	}
	if p.Category != "Huevos leche y mantequilla / Leche" {
		t.Errorf("categoría = %q", p.Category)
	}
}

func TestParseProductSinJSONLD(t *testing.T) {
	if _, err := ParseProduct(`<html><body>nada</body></html>`, "https://www.dia.es/a/b/p/1"); err == nil {
		t.Error("una página sin datos de producto debe fallar")
	}
}

func TestCategoryFromURL(t *testing.T) {
	casos := []struct{ url, want string }{
		{"https://www.dia.es/huevos-leche-y-mantequilla/leche/p/16065", "huevos-leche-y-mantequilla/leche"},
		{"https://www.dia.es/limpieza-y-hogar/insecticidas/p/272250", "limpieza-y-hogar/insecticidas"},
		{"https://www.dia.es/p/42", ""},
	}
	for _, c := range casos {
		if got := categoryFromURL(c.url); got != c.want {
			t.Errorf("categoryFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// TestCategoryNameFromURL comprueba la categoría legible completa. Se
// humaniza cada nivel por separado y se unen con " / ": es la única parte de la
// URL de DÍA que describe la sección, así que perder el orden de los niveles
// (que es el orden de la URL) haría que dos secciones distintas se leyeran
// igual. Dentro de un nivel no se añade puntuación —"Huevos leche y
// mantequilla" y no "Huevos, leche y mantequilla"— porque el slug solo
// transmite el texto y las comas habría que inventarlas.
func TestCategoryNameFromURL(t *testing.T) {
	casos := []struct{ url, want string }{
		{"https://www.dia.es/huevos-leche-y-mantequilla/leche/p/16065", "Huevos leche y mantequilla / Leche"},
		{"https://www.dia.es/limpieza-y-hogar/insecticidas/p/272250", "Limpieza y hogar / Insecticidas"},
		{"https://www.dia.es/p/42", ""},
	}
	for _, c := range casos {
		got := categoryNameFromURL(c.url)
		if got != c.want {
			t.Errorf("categoryNameFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestProductID(t *testing.T) {
	if got := productID("https://www.dia.es/a/b/p/173903P6"); got != "173903P6" {
		t.Errorf("productID = %q", got)
	}
}

// TestSample comprueba que el muestreo reparte las fichas de una categoría en
// lugar de devolver siempre las primeras.
func TestSample(t *testing.T) {
	var entries []chain.SitemapEntry
	for i := 0; i < 100; i++ {
		entries = append(entries, chain.SitemapEntry{URL: "https://www.dia.es/a/b/p/" + itoa(i)})
	}
	got := sample(entries, 5)
	if len(got) != 5 {
		t.Fatalf("sample devolvió %d entradas, want 5", len(got))
	}
	if got[0].URL != entries[0].URL {
		t.Errorf("la primera ficha debería ser la primera: %s", got[0].URL)
	}
	if got[4].URL == entries[4].URL {
		t.Error("la última ficha no debería ser la quinta: el muestreo no se está repartiendo")
	}
}

func TestSampleCategoríaPequeña(t *testing.T) {
	entries := []chain.SitemapEntry{{URL: "a"}, {URL: "b"}}
	if got := sample(entries, 5); len(got) != 2 {
		t.Errorf("con menos fichas que el límite debe devolverlas todas, devolvió %d", len(got))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
