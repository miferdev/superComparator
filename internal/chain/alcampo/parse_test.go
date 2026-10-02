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
	// El JSON-LD da el precio de la unidad y el €/litro va aparte: no se toca
	// este adaptador porque su WAF no deja guardar una ficha real con la que
	// comprobar qué publica un producto vendido al peso.
	if p.PrecioEsPorMedida() {
		t.Error("1,09 € es el precio de la unidad, no un precio por medida")
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

// TestParseProductDesescapaEntidades: el JSON-LD de Alcampo es texto plano para
// el parser de HTML, así que sus entidades ("Hellmann&#039;s") llegaban tal cual
// a products.name. El precio viene en el propio JSON-LD y no se toca al
// desescapar el texto.
func TestParseProductDesescapaEntidades(t *testing.T) {
	const conEntidades = `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org/","@type":"Product","name":"Salsa barbacoa Hellmann&#039;s 285 g &amp; Guacamole",
	 "sku":"91001","brand":{"@type":"Brand","name":"Hellmann&#x27;s &amp; Co"},
	 "offers":{"@type":"Offer","price":1.65,"priceCurrency":"EUR","availability":"https://schema.org/InStock"}}
	</script></head><body><h1>Salsa barbacoa Hellmann's 285 g &amp; Guacamole</h1></body></html>`
	p, err := ParseProduct(conEntidades, "https://www.compraonline.alcampo.es/products/salsa-barbacoa-hellmanns/91001")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Salsa barbacoa Hellmann's 285 g & Guacamole" {
		t.Errorf("nombre = %q", p.Name)
	}
	if p.Brand != "Hellmann's & Co" {
		t.Errorf("marca = %q", p.Brand)
	}
	if p.Price != 1.65 {
		t.Errorf("precio = %v, want 1.65 (el precio no se toca)", p.Price)
	}
}

// TestParseProductNoDesescapaDosVeces: "&amp;lt;" en el JSON-LD quiere decir
// "&lt;" escrito, así que el resultado es "&lt;" y no "<". El caso del texto que
// el parser de HTML ya devolvió desescapado una vez (el DOM) se cubre en
// mercadona, que sí tiene ficha real guardada.
func TestParseProductNoDesescapaDosVeces(t *testing.T) {
	const conDoble = `<script type="application/ld+json">{"@type":"Product","name":"Cacao &amp;lt; 100 g",
	 "brand":{"@type":"Brand","name":"Marca &amp;amp;"},
	 "offers":{"price":"2.50","availability":"http://schema.org/InStock"}}</script>`
	p, err := ParseProduct(conDoble, "https://www.compraonline.alcampo.es/products/cacao-lt-100g/91002")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Cacao &lt; 100 g" {
		t.Errorf("nombre = %q, want %q", p.Name, "Cacao &lt; 100 g")
	}
	if p.Brand != "Marca &amp;" {
		t.Errorf("marca = %q, want %q", p.Brand, "Marca &amp;")
	}
	if p.Price != 2.5 {
		t.Errorf("precio = %v, want 2.5 (el precio no se toca)", p.Price)
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
