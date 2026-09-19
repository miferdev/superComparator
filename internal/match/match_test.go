package match

import (
	"testing"

	"github.com/miferdev/superComparator/internal/chain"
)

func entries(items ...string) []chain.SitemapEntry {
	out := make([]chain.SitemapEntry, 0, len(items))
	for _, it := range items {
		out = append(out, chain.SitemapEntry{URL: it, Name: it})
	}
	return out
}

func TestRankBolsasBasura(t *testing.T) {
	got := Rank("2x bolsas basura", entries(
		"sazonador ajo limon hacendado carne pescado con bolsa",
		"bolsa basura grande bosque verde 50l cubo grande paquete",
		"bolsas de basura lanta 30 litros reciclado de envases",
	), 3)
	if len(got) == 0 {
		t.Fatal("sin candidatos")
	}
	if got[0].Entry.URL == "sazonador ajo limon hacendado carne pescado con bolsa" {
		t.Fatalf("primer candidato incorrecto: %+v", got)
	}
}

func TestRankPanalesYChampu(t *testing.T) {
	got := Rank("pañales", entries("panal dodot sensitive 58 unidades talla 2"), 3)
	if len(got) != 1 {
		t.Fatalf("candidatos = %+v", got)
	}
	got = Rank("champú", entries("champu anticaida deliplus con ginseng", "gel de ducha"), 3)
	if len(got) != 1 || got[0].Entry.URL != "champu anticaida deliplus con ginseng" {
		t.Fatalf("candidatos = %+v", got)
	}
}

func TestRankSizeToken(t *testing.T) {
	got := Rank("leche entera 1l", entries(
		"leche entera hacendado brick 1 l",
		"leche entera hacendado brick 1,5 l",
	), 3)
	if len(got) == 0 || got[0].Entry.URL != "leche entera hacendado brick 1 l" {
		t.Fatalf("candidatos = %+v", got)
	}
}

func TestSimilarity(t *testing.T) {
	high := Similarity("leche entera 1l", "Leche entera Hacendado", "Brik 1 L")
	pack := Similarity("leche entera 1l", "Leche entera Asturiana", "6 botellas x 1,5 L")
	low := Similarity("leche entera 1l", "Leche desnatada Pascual", "1 L")
	if high <= low {
		t.Fatalf("high=%v low=%v", high, low)
	}
	if high <= pack {
		t.Fatalf("un pack de 9 l no debería superar a un brik de 1 l: high=%v pack=%v", high, pack)
	}
	if Similarity("pan 1kg", "Pan de molde", "500 g") >= high {
		t.Fatal("el tamaño distinto debería penalizar")
	}
}

func TestParseMeasure(t *testing.T) {
	cases := []struct {
		in    string
		value float64
		unit  string
	}{
		{"Brik 1 L", 1, "l"},
		{"6 botellas x 1,5 L", 9, "l"},
		{"6 mini briks x 200 ml", 1.2, "l"},
		{"Paquete 400 g", 0.4, "kg"},
		{"2 x 500 g", 1, "kg"},
	}
	for _, c := range cases {
		m, ok := ParseMeasure(c.in)
		if !ok || m.Unit != c.unit || m.Value < c.value-1e-9 || m.Value > c.value+1e-9 {
			t.Errorf("ParseMeasure(%q) = %+v, %v; want %v %s", c.in, m, ok, c.value, c.unit)
		}
	}
	if _, ok := ParseMeasure("sin medida"); ok {
		t.Error("no debería encontrar medida")
	}
}
