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
	if p.PrecioEsPorMedida() {
		t.Error("1,24 € es el precio de la botella; el €/LITRO es solo el desglose")
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

// TestParseProductPrecioPorPeso cubre el producto que DÍA vende al peso: el
// precio publicado lleva la unidad encima, así que Price queda vacío y lo
// único que hay es el €/kg. La ficha no da error por ello.
func TestParseProductPrecioPorPeso(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org/","@type":"Product","name":"Plátano de Canarias IGP",
	 "offers":{"@type":"Offer","price":1.65,"priceCurrency":"EUR","availability":"https://schema.org/InStock"}}
	</script></head><body>
	<div class="buy-box__prices">
		<p class="buy-box__active-price">1,65&nbsp;€/KG</p>
		<p class="buy-box__price-per-unit"> (1,65&nbsp;€/KG) </p>
	</div></body></html>`
	p, err := ParseProduct(html, "https://www.dia.es/frutas-y-verduras/platano/p/90001")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Price != 0 || p.UnitPrice != 0 {
		t.Errorf("un €/KG no es un precio de unidad: Price = %v, UnitPrice = %v", p.Price, p.UnitPrice)
	}
	if p.MeasurePrice != 1.65 || p.MeasureUnit != "kg" {
		t.Errorf("precio por medida = %v %q, want 1.65 kg", p.MeasurePrice, p.MeasureUnit)
	}
	if !p.PrecioEsPorMedida() {
		t.Error("1,65 €/KG es un precio por medida")
	}
	if p.Category != "Frutas y verduras / Platano" {
		t.Errorf("categoría = %q", p.Category)
	}
}

// TestParseProductDesescapaEntidades cubre el fallo que motivó el cambio: el
// JSON-LD es texto plano para el parser de HTML, así que sus entidades llegaban
// tal cual a products.name ("Hellmann&#039;s") y el usuario no encontraba el
// producto buscando el apóstrofo. El precio sale del dato estructurado y no se
// toca: 1,65 sigue siendo 1,65.
func TestParseProductDesescapaEntidades(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org/","@type":"Product",
	 "name":"Salsa barbacoa Hellmann&#039;s 285 g &amp; Guacamole",
	 "sku":"91001","brand":{"@type":"Brand","name":"Hellmann&#x27;s &amp; Co"},
	 "offers":{"@type":"Offer","price":1.65,"priceCurrency":"EUR","availability":"https://schema.org/InStock"}}
	</script></head><body>
	<div class="buy-box__prices">
		<p class="buy-box__active-price">1,65&nbsp;€/ud</p>
		<p class="buy-box__price-per-unit">1,65&nbsp;€/ud</p>
	</div></body></html>`
	p, err := ParseProduct(html, "https://www.dia.es/salsas-y-condimentos/salsas/p/91001")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Salsa barbacoa Hellmann's 285 g & Guacamole" {
		t.Errorf("nombre = %q", p.Name)
	}
	if p.Brand != "Hellmann's & Co" {
		t.Errorf("marca = %q", p.Brand)
	}
	if p.Price != 1.65 || p.UnitPrice != 1.65 {
		t.Errorf("el precio no se toca: Price = %v, UnitPrice = %v, want 1.65", p.Price, p.UnitPrice)
	}
	// "/ud" no es una medida continua: desescapar no puede inventarla.
	if p.MeasurePrice != 0 || p.MeasureUnit != "" {
		t.Errorf("medida = %v %q, want vacío", p.MeasurePrice, p.MeasureUnit)
	}
	if p.Format != "285 g" {
		t.Errorf("formato = %q, want 285 g", p.Format)
	}
}

// TestParseProductNoDesescapaDosVeces es el test que impide arreglar el doble
// desescapo: en el JSON-LD "&amp;lt;" quiere decir "&lt;" escrito, así que el
// resultado es "&lt;" y no "<". Desescapar el texto compuesto entero, en vez de
// solo el JSON-LD, convertiría esto en "<".
func TestParseProductNoDesescapaDosVeces(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org/","@type":"Product","name":"Cacao &amp;lt; 100 g &amp; chocolate",
	 "brand":{"@type":"Brand","name":"Marca &amp;amp;"},
	 "offers":{"@type":"Offer","price":2.5,"priceCurrency":"EUR","availability":"https://schema.org/InStock"}}
	</script></head><body></body></html>`
	p, err := ParseProduct(html, "https://www.dia.es/desayunos/cacao/p/91002")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Cacao &lt; 100 g & chocolate" {
		t.Errorf("nombre = %q, want %q", p.Name, "Cacao &lt; 100 g & chocolate")
	}
	if p.Brand != "Marca &amp;" {
		t.Errorf("marca = %q, want %q", p.Brand, "Marca &amp;")
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
