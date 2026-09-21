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
	if p.MeasurePrice != 2.45 || p.MeasureUnit != "l" {
		t.Errorf("medida = %v %s", p.MeasurePrice, p.MeasureUnit)
	}
	if p.Format != "Brik 1 L" {
		t.Errorf("Format = %q", p.Format)
	}
	if !p.Available {
		t.Error("Available = false")
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
}
