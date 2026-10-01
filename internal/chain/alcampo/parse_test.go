package alcampo

import (
	"testing"
)

// La web de Alcampo está detrás de un WAF que responde 403 a las peticiones
// automatizadas, así que no se ha podido guardar una ficha real en testdata.
// Estos tests cubren el análisis del JSON-LD que publica (schema.org Product,
// con el precio como número) y no el HTML concreto de su storefront.
const ficha = `<html><head>
<script type="application/ld+json">
{"@context":"https://schema.org/","@type":"Product","name":"Leche entera Central Lechera Asturiana 1 L",
 "sku":"90610","brand":{"@type":"Brand","name":"Central Lechera Asturiana"},
 "offers":{"@type":"Offer","price":1.09,"priceCurrency":"EUR","availability":"https://schema.org/InStock"}}
</script></head><body>
<h1>Leche entera Central Lechera Asturiana 1 L</h1>
<p class="price-per-unit">1,09 €/litro</p>
</body></html>`

func TestParseProduct(t *testing.T) {
	const url = "https://www.compraonline.alcampo.es/products/central-lechera-asturiana-leche-desnatada-1-l/90610"
	p, err := ParseProduct(ficha, url)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Leche entera Central Lechera Asturiana 1 L" {
		t.Errorf("nombre = %q", p.Name)
	}
	if p.Price != 1.09 {
		t.Errorf("precio = %v, want 1.09", p.Price)
	}
	if p.Brand != "Central Lechera Asturiana" {
		t.Errorf("marca = %q", p.Brand)
	}
	if p.SKU != "90610" {
		t.Errorf("SKU = %q, want 90610 (el del JSON-LD)", p.SKU)
	}
	if !p.Available {
		t.Error("InStock debería estar disponible")
	}
	if p.MeasurePrice != 1.09 || p.MeasureUnit != "l" {
		t.Errorf("precio por unidad = %v %q, want 1.09 l", p.MeasurePrice, p.MeasureUnit)
	}
	if p.Format != "1 L" {
		t.Errorf("formato = %q, want 1 L", p.Format)
	}
	if p.Chain != "alcampo" {
		t.Errorf("cadena = %q", p.Chain)
	}
}

func TestParseProductAgotado(t *testing.T) {
	agotado := `<script type="application/ld+json">{"@type":"Product","name":"Pan","offers":{"price":"2.00","availability":"http://schema.org/OutOfStock"}}</script>`
	p, err := ParseProduct(agotado, "https://www.compraonline.alcampo.es/products/pan/1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Available {
		t.Error("OutOfStock no debería estar disponible")
	}
}

func TestParseProductSinJSONLD(t *testing.T) {
	if _, err := ParseProduct(`<html><body>blocked</body></html>`, "https://x/products/y/1"); err == nil {
		t.Error("una página sin datos de producto debe fallar")
	}
}

func TestEntryFromURL(t *testing.T) {
	e := entryFromURL("https://www.compraonline.alcampo.es/products/larsa-leche-entera-de-vaca-1-l/51814", "")
	if e.Name != "larsa leche entera de vaca 1 l" {
		t.Errorf("nombre = %q", e.Name)
	}
	if e.SKU != "51814" {
		t.Errorf("SKU = %q", e.SKU)
	}
}
