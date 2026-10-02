package mercadona

import (
	"os"
	"testing"
)

func TestParseProduct(t *testing.T) {
	html, err := os.ReadFile("../../../testdata/mercadona_product.html")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p, err := ParseProduct(string(html), "https://tienda.mercadona.es/product/10005/chocolate-liquido-taza-hacendado-brick")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Chocolate líquido a la taza Hacendado" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.SKU != "10005" {
		t.Errorf("SKU = %q", p.SKU)
	}
	if p.Price != 2.45 {
		t.Errorf("Price = %v", p.Price)
	}
	if p.UnitPrice != 2.45 {
		t.Errorf("UnitPrice = %v, want 2.45", p.UnitPrice)
	}
	if p.MeasurePrice != 2.45 || p.MeasureUnit != "l" {
		t.Errorf("medida = %v %s", p.MeasurePrice, p.MeasureUnit)
	}
	// El precio viene por unidad ("2,45 €" y "/ud."); el €/L del formato es solo
	// el desglose, así que el bueno es el de la unidad.
	if p.PrecioEsPorMedida() {
		t.Error("2,45 € es un precio de unidad, no un precio por medida")
	}
	// La miga de esta ficha real llega como "Cacao, café e infusiones &gt;":
	// el parser de HTML ya la devuelve con la entidad desescapada, y por eso este
	// adaptador no vuelve a desescapar nada.
	if p.Category != "Cacao, café e infusiones / Cacao soluble y chocolate a la taza" {
		t.Errorf("Category = %q", p.Category)
	}
	if p.Format != "Brik 1 L" {
		t.Errorf("Format = %q", p.Format)
	}
	if !p.Available {
		t.Error("Available = false")
	}
}

// TestParseProductNoDesescapaElDOM fija el otro lado del problema: el texto del
// DOM ya viene desescapado por el parser de HTML, y volver a desescaparlo sería
// el doble desescapo. Aquí "&amp;" sale "&" (una vez, por el parser) y
// "&amp;lt;", que en la ficha significa "&lt;" escrito, sale "&lt;" y no "<".
// Mercadona no lee nada del JSON-LD, así que este adaptador no desescapa nada:
// solo lo que el parser de HTML no interpretó.
func TestParseProductNoDesescapaElDOM(t *testing.T) {
	const html = `<div class="private-product-detail">
		<h1 class="private-product-detail__description">Cacao &amp; chocolate &amp;lt; 100 g</h1>
		<p class="product-price__unit-price">1,25 €</p>
	</div>`
	p, err := ParseProduct(html, "https://tienda.mercadona.es/product/12345/cacao-chocolate")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Name != "Cacao & chocolate &lt; 100 g" {
		t.Errorf("Name = %q, want %q", p.Name, "Cacao & chocolate &lt; 100 g")
	}
	if p.Format != "100 g" {
		t.Errorf("Format = %q, want 100 g", p.Format)
	}
}

func TestParseProductSinMedidaNoInventaPrecio(t *testing.T) {
	const html = `<div class="private-product-detail">
		<h1 class="private-product-detail__description">Arroz redondo</h1>
		<div class="product-format__size"><span aria-hidden="true">Paquete 1 kg</span></div>
		<p class="product-price__unit-price">1,25 €</p>
		<p class="product-price__extra-price">/kg</p>
	</div>`
	p, err := ParseProduct(html, "https://tienda.mercadona.es/product/12345/arroz-redondo")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.MeasurePrice != 0 || p.MeasureUnit != "" {
		t.Errorf("no debe inferir €/medida: %v %q", p.MeasurePrice, p.MeasureUnit)
	}
	if p.PrecioEsPorMedida() {
		t.Error("sin precio por medida publicado no puede serlo")
	}
}

// TestParseProductSinMigaNoInventaCategoria comprueba que, si la ficha no trae
// miga de pan, la categoría se queda vacía en vez de deducirse del nombre.
func TestParseProductSinMigaNoInventaCategoria(t *testing.T) {
	const html = `<div class="private-product-detail">
		<h1 class="private-product-detail__description">Arroz redondo</h1>
		<p class="product-price__unit-price">1,25 €</p>
	</div>`
	p, err := ParseProduct(html, "https://tienda.mercadona.es/product/12345/arroz-redondo")
	if err != nil {
		t.Fatalf("ParseProduct: %v", err)
	}
	if p.Category != "" {
		t.Errorf("Category = %q, want vacío", p.Category)
	}
}

// TestParseProductPrecioPorPeso cubre los productos que Mercadona vende al
// peso: el precio viene como €/kg, así que es un precio por medida y el precio
// de unidad se deja vacío en lugar de servir el del kilo como el de una bolsa.
func TestParseProductPrecioPorPeso(t *testing.T) {
	const html = `<div class="private-product-detail">
		<h1 class="private-product-detail__description">Plátano de Canarias IGP</h1>
		<div class="product-format__size"><span aria-hidden="true">Pieza</span></div>
		<p class="product-price__unit-price">1,65<span>€/kg</span></p>
	</div>`
	p, err := ParseProduct(html, "https://tienda.mercadona.es/product/3819/platano-canarias-igp-pieza")
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
		t.Error("1,65 €/kg es un precio por medida")
	}
	if !p.Available {
		t.Error("el producto con precio está disponible")
	}
}
