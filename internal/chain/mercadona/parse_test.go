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
