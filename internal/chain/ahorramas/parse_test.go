package ahorramas

import (
	"os"
	"strings"
	"testing"
)

func TestParseProduct(t *testing.T) {
	html, err := os.ReadFile("../../../testdata/ahorramas_product.html")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p, err := ParseProduct(string(html), "https://www.ahorramas.com/garbanzo-cocido-luengo-400g-44569.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Garbanzo cocido Luengo 400g" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.SKU != "44569" {
		t.Errorf("SKU = %q", p.SKU)
	}
	if p.Brand != "LUENGO" {
		t.Errorf("Brand = %q", p.Brand)
	}
	// El bloque de precio de esta ficha real escribe los euros como entidad
	// ("1,00&euro;", "3,25&euro;", "2,50&euro;/KG.PESO ESC"): el parser de HTML
	// las resuelve y los precios salen bien. Con esto, desescapar el nombre del
	// JSON-LD no puede haber tocado ni el precio ni la medida.
	if p.Price != 1.00 {
		t.Errorf("Price = %v", p.Price)
	}
	if p.UnitPrice != 1.00 {
		t.Errorf("UnitPrice = %v, want 1.00", p.UnitPrice)
	}
	if p.PrecioEsPorMedida() {
		t.Error("1,00 € es el precio de la bolsa; el 2,50 €/kg es solo el desglose")
	}
	if p.OldPrice != 3.25 {
		t.Errorf("OldPrice = %v", p.OldPrice)
	}
	if p.MeasurePrice != 2.50 || p.MeasureUnit != "kg" {
		t.Errorf("medida = %v %s", p.MeasurePrice, p.MeasureUnit)
	}
	if !p.Available {
		t.Error("Available = false")
	}
	if !strings.Contains(p.PromoText, "Bajada de precio") {
		t.Errorf("PromoText = %q", p.PromoText)
	}
	if p.Format != "0,400 KG" {
		t.Errorf("Format = %q", p.Format)
	}
	if !p.HasPromo() {
		t.Error("se esperaba HasPromo")
	}
	if p.Category != "Alimentación / Arroces, Pastas y Legumbres / Legumbres" {
		t.Errorf("Category = %q", p.Category)
	}
}

// TestParseProductDesescapaEntidades cubre el nombre, la marca y la miga de pan,
// que en Ahorramas salen del JSON-LD: es texto plano para el parser de HTML, así
// que sus entidades ("Hellmann&#039;s") llegaban tal cual a la base. El precio y
// la medida salen del DOM y no se tocan.
func TestParseProductDesescapaEntidades(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"Product","name":"Salsa barbacoa Hellmann&#039;s 285 g","sku":"91001",
	 "brand":{"@type":"Brand","name":"Hellmann&#x27;s &amp; Co"},
	 "offers":{"@type":"Offer","priceCurrency":"EUR","price":"1.65","availability":"http://schema.org/InStock"}}
	</script>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"BreadcrumbList","itemListElement":[
	 {"@type":"ListItem","position":1,"item":{"name":"Alimentaci&oacute;n"}},
	 {"@type":"ListItem","position":2,"item":{"name":"Salsas &amp; Condimentos"}}]}
	</script></head><body>
	<div class="product"><div class="prices">
		<div class="price"><span class="sales"><span class="value" content="1.65">1<span class="decimal-part">,65&euro;</span></span></span></div>
		<div class="unit-price-row price"><span class="unit-price-per-unit grey">1,65&euro;/KG</span></div>
	</div></div></body></html>`
	p, err := ParseProduct(html, "https://www.ahorramas.com/salsa-barbacoa-hellmanns-285g-91001.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Salsa barbacoa Hellmann's 285 g" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Brand != "Hellmann's & Co" {
		t.Errorf("Brand = %q", p.Brand)
	}
	if p.Category != "Alimentación / Salsas & Condimentos" {
		t.Errorf("Category = %q", p.Category)
	}
	// La medida sale del bloque de precio del DOM, con su "&euro;", y no se toca
	// al desescapar: el €/kg se sigue leyendo igual.
	if p.MeasurePrice != 1.65 || p.MeasureUnit != "kg" {
		t.Errorf("medida = %v %s, want 1.65 kg (la medida no se toca)", p.MeasurePrice, p.MeasureUnit)
	}
	if p.PrecioEsPorMedida() {
		t.Error("1,65 €/KG junto al precio de la bolsa es solo el desglose")
	}
}

// TestParseProductPrecioJSONLDConDecimales es el test del bug del precio: el
// JSON-LD publica el precio como número de máquina ("1.65"), con punto decimal,
// y antes pasaba por el parser de precios en español, que corta en el punto. Un
// 1,65 se guardaba como 1 y el 40 % del precio se perdía en silencio: sin error,
// directo a la base. El HTML es el mínimo que publicaría la tienda, escrito en el
// test porque la ficha de testdata es de un producto de 1,00 € y con ella el
// fallo no se ve.
func TestParseProductPrecioJSONLDConDecimales(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"Product","name":"Salsa barbacoa Hellmann's 285 g","sku":"91001",
	 "offers":{"@type":"Offer","priceCurrency":"EUR","price":"1.65","availability":"http://schema.org/InStock"}}
	</script></head><body>
	<div class="price"><span class="sales">1,65&euro;</span></div></body></html>`
	p, err := ParseProduct(html, "https://www.ahorramas.com/salsa-barbacoa-hellmanns-285g-91001.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	// El "1" que salía antes no era un redondeo: era el precio perder los céntimos.
	if p.Price != 1.65 {
		t.Errorf("Price = %v, want 1.65 (el JSON-LD va en formato máquina)", p.Price)
	}
	if p.UnitPrice != 1.65 {
		t.Errorf("UnitPrice = %v, want 1.65", p.UnitPrice)
	}
	if p.Price == 1 {
		t.Error("Price = 1: los céntimos del JSON-LD se han perdido")
	}
}

// TestParseProductPrecioJSONLDSinComillas cubre el otro modo en que las tiendas
// pueden publicar el precio: como número JSON en vez de como texto. La ficha no
// se descarta: se guarda tal cual la da la tienda y el mismo 1,65 sale igual.
func TestParseProductPrecioJSONLDSinComillas(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"Product","name":"Salsa barbacoa Hellmann's 285 g","sku":"91001",
	 "offers":{"@type":"Offer","priceCurrency":"EUR","price":1.65,"availability":"http://schema.org/InStock"}}
	</script></head><body>
	<div class="price"><span class="sales">1,65&euro;</span></div></body></html>`
	p, err := ParseProduct(html, "https://www.ahorramas.com/salsa-barbacoa-hellmanns-285g-91001.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Price != 1.65 {
		t.Errorf("Price = %v, want 1.65 (precio JSON numérico)", p.Price)
	}
}

// TestParseProductPrecioVisibleEnEspanol es la contraprueba: en la misma ficha
// conviven los dos formatos y cada uno se tiene que leer con su parser. El
// precio de la bolsa llega del JSON-LD en máquina ("1.65") y el precio anterior
// se lee del texto visible de la web, en español ("3,25 €"), así que si se
// mezclan los dos el resultado sale 1 o 3.25 en el sitio equivocado.
func TestParseProductPrecioVisibleEnEspanol(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"Product","name":"Salsa barbacoa Hellmann's 285 g","sku":"91001",
	 "offers":{"@type":"Offer","priceCurrency":"EUR","price":"1.65","availability":"http://schema.org/InStock"}}
	</script></head><body>
	<div class="prices">
		<div class="price-old">3,25&euro;</div>
		<div class="price"><span class="sales"><span class="value">1<span class="decimal-part">,65&euro;</span></span></span></div>
		<div class="unit-price-row price"><span class="unit-price-per-unit grey">1,65&euro;/KG</span></div>
	</div></body></html>`
	p, err := ParseProduct(html, "https://www.ahorramas.com/salsa-barbacoa-hellmanns-285g-91001.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Price != 1.65 {
		t.Errorf("Price = %v, want 1.65", p.Price)
	}
	// Este viene del HTML visible, con coma decimal: ParsePrice sigue siendo su
	// parser y no se ha tocado por cambiar el del JSON-LD.
	if p.OldPrice != 3.25 {
		t.Errorf("OldPrice = %v, want 3.25 (el texto visible es español)", p.OldPrice)
	}
	if p.MeasurePrice != 1.65 || p.MeasureUnit != "kg" {
		t.Errorf("medida = %v %q, want 1.65 kg", p.MeasurePrice, p.MeasureUnit)
	}
}

// TestParseProductNoDesescapaDosVeces es el test que impide arreglar el doble
// desescapo: "&amp;lt;" en el JSON-LD quiere decir "&lt;" escrito, no "<".
func TestParseProductNoDesescapaDosVeces(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"Product","name":"Cacao &amp;lt; 100 g","sku":"91002",
	 "brand":{"@type":"Brand","name":"Marca &amp;amp;"},
	 "offers":{"@type":"Offer","priceCurrency":"EUR","price":"2.50","availability":"http://schema.org/InStock"}}
	</script></head><body><div class="price"><span class="sales">2,50&euro;</span></div></body></html>`
	p, err := ParseProduct(html, "https://www.ahorramas.com/cacao-lt-100g-91002.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Cacao &lt; 100 g" {
		t.Errorf("Name = %q, want %q", p.Name, "Cacao &lt; 100 g")
	}
	if p.Brand != "Marca &amp;" {
		t.Errorf("Brand = %q, want %q", p.Brand, "Marca &amp;")
	}
}

// TestParseProductPrecioPorPeso cubre el caso en que el propio precio publicado
// viene por kilo: el JSON-LD trae el número del €/kg y el bloque de precio lo
// enseña con la unidad, así que no hay precio de unidad que guardar. El HTML es
// el que publicaría la tienda, escrito a mano: la ficha guardada en testdata es
// de un producto por unidades y no llega a este caso.
func TestParseProductPrecioPorPeso(t *testing.T) {
	const html = `<html><head>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"Product","name":"Plátano de Canarias IGP","sku":"3819",
	 "brand":{"@type":"Brand","name":"Favorit"},
	 "offers":{"@type":"Offer","priceCurrency":"EUR","price":"1.65","availability":"http://schema.org/InStock"}}
	</script>
	<script type="application/ld+json">
	{"@context":"http://schema.org/","@type":"BreadcrumbList","itemListElement":[
	 {"@type":"ListItem","position":1,"item":{"name":"Frutas y Verduras"}},
	 {"@type":"ListItem","position":2,"item":{"name":"Plátanos"}}]}
	</script></head><body>
	<div class="product"><h1>Plátano de Canarias IGP</h1>
	<div class="prices">
		<div class="price"><span class="sales"><span class="value" content="1.65">1<span class="decimal-part">,65&euro;/KG</span></span></span></div>
		<div class="unit-price-row price"><span class="unit-price-per-unit grey">1,65&euro;/KG.PESO ESC</span></div>
	</div></div></body></html>`
	p, err := ParseProduct(html, "https://www.ahorramas.com/platano-canarias-igp-3819.html")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Price != 0 || p.UnitPrice != 0 {
		t.Errorf("un €/kg no es un precio de unidad: Price = %v, UnitPrice = %v", p.Price, p.UnitPrice)
	}
	if p.MeasurePrice != 1.65 || p.MeasureUnit != "kg" {
		t.Errorf("precio por medida = %v %q, want 1.65 kg", p.MeasurePrice, p.MeasureUnit)
	}
	if !p.PrecioEsPorMedida() {
		t.Error("1,65 €/KG es un precio por medida")
	}
	if p.Category != "Frutas y Verduras / Plátanos" {
		t.Errorf("Category = %q", p.Category)
	}
	if !p.Available {
		t.Error("InStock debería estar disponible")
	}
}
