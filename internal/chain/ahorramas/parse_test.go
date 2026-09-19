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
	if p.Price != 1.00 {
		t.Errorf("Price = %v", p.Price)
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
}
