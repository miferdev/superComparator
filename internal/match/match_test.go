package match

import (
	"strings"
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
		{"20 cl", 0.2, "l"},
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

func TestSimilarityToleraMarca(t *testing.T) {
	got := Similarity("leche entera 1 l", "Leche entera Hacendado Brick 1 L", "Brik 1 L")
	if got < 0.8 {
		t.Fatalf("la marca extra no debe hundir la coincidencia: %v", got)
	}
}

func TestSinonimos(t *testing.T) {
	if got := Tokens("refresco de cola"); len(got) != 2 || got[0] != "bebida" || got[1] != "cola" {
		t.Fatalf("Tokens = %v", got)
	}
	got := Rank("refresco cola", entries("bebida cola zero"), 3)
	if len(got) != 1 {
		t.Fatalf("no se unificó el sinónimo: %+v", got)
	}
}

// TestSimilarityNoAceptaPorLaMedida comprueba que coincidir solo en el
// formato no convierte un producto distinto en el buscado: fue el caso de la
// leche infantil de continuación para "leche semidesnatada 1L".
func TestSimilarityNoAceptaPorLaMedida(t *testing.T) {
	casos := []struct {
		nombre, producto, formato string
		quiere                    bool
	}{
		{"Leche semidesnatada 1L", "Leche semidesnatada Hacendado", "1 L", true},
		{"Leche semidesnatada 1L", "Leche Asturiana 1l semidesnatada", "1 L", true},
		{"Leche semidesnatada 1L", "Leche infantil de continuación de 6 a 12 meses Nativa", "1 L", false},
		{"Leche entera 1L", "Leche entera Asturiana 1 L", "1 L", true},
		{"Pan de molde blanco", "Pan de molde blanco Hacendado", "", true},
		{"Pan de molde blanco", "Pan de molde sin corteza Alipende 450g", "450 g", true},
		{"copos de avena suaves", "Copos de avena Brüggen", "", true},
		{"copos de avena suaves", "Copos avena integrales sin gluten bio Ecocesta 500g", "500 g", false},
		{"Kéfir natural", "Kéfir de fresa y frambuesa Activia 4 x 125 g", "4 x 125 g", false},
	}
	for _, c := range casos {
		got := Similarity(c.nombre, c.producto, c.formato)
		aceptado := got >= AutoThreshold
		if aceptado != c.quiere {
			t.Errorf("Similarity(%q, %q, %q) = %.2f (aceptado=%v), quiero aceptado=%v",
				c.nombre, c.producto, c.formato, got, aceptado, c.quiere)
		}
	}
}

// TestSimilarityPack rechaza los packs cuando no se han pedido: aceptarlos
// multiplicaría la cantidad equivocada en el total de la compra.
func TestSimilarityPack(t *testing.T) {
	conPack := Similarity("Leche entera 1L", "Leche entera Asturiana pack 6 x 1 L", "6 x 1 L")
	if conPack >= AutoThreshold {
		t.Errorf("un pack de 6 unidades no debería encajar con 1 L: %.2f", conPack)
	}
	// Si el usuario pide packs, sí.
	pedido := Similarity("Leche entera pack 6 x 1 L", "Leche entera Asturiana pack 6 x 1 L", "6 x 1 L")
	if pedido < AutoThreshold {
		t.Errorf("un pack pedido explícitamente debería encajar, %.2f", pedido)
	}
}

// TestSimilarityFormatoDistinto cubre el caso de un pack de 13 L para pedir 1 L.
func TestSimilarityFormatoDistinto(t *testing.T) {
	casos := []struct {
		nombre, producto, formato string
		quiere                    bool
	}{
		{"Leche semidesnatada 1L", "Leche semidesnatada Asturiana pack 6 x 2.2 L", "6 x 2.2 L", false},
		{"Leche entera 1L", "Leche entera Asturiana 1 L", "1 L", true},
		{"Leche entera 1L", "Leche entera Asturiana pack 6 x 1 L", "6 x 1 L", false},
		{"Copos de avena 500 g", "Copos de avena Azucarados 1 kg", "1 kg", true},
		{"Copos de avena 500 g", "Copos de avena Azucarados 500 g", "500 g", true},
		{"Pan de molde 400 g", "Pan de molde familiar 550 g", "550 g", true},
	}
	for _, c := range casos {
		got := Similarity(c.nombre, c.producto, c.formato)
		if (got >= AutoThreshold) != c.quiere {
			t.Errorf("Similarity(%q, %q, %q) = %.2f, quiero aceptado=%v", c.nombre, c.producto, c.formato, got, c.quiere)
		}
	}
}

// TestSimilarityProductoQueLoLlevaDentro cubre el caso de «fresas», que se
// emparejaba con «mermelada de fresa», y el de «pipas de calabaza», que se
// emparejaba con «pan con pipas de calabaza». En ambos, el candidato es otro
// producto que contiene lo pedido.
func TestSimilarityProductoQueLoLlevaDentro(t *testing.T) {
	casos := []struct {
		nombre, producto, formato string
		quiere                    bool
	}{
		{"fresas", "Mermelada de fresa Hacendado", "340 g", false},
		{"fresas", "Fresas Hacendado 500 g", "500 g", true},
		{"fresas", "Sirope Alipende 300g fresa", "300 g", false},
		{"pipas de calabaza", "Pan de molde semillas y pipas de calabaza Hacendado", "", false},
		{"pipas de calabaza", "Pipas de calabaza peladas 100 g", "100 g", true},
		{"pipas de calabaza", "Calabaza 1,6 Kg aprox.", "1,6 kg", false},
		{"alcachofa congelada", "Alcachofa Troceada Hacendado Ultracongelada Paquete", "1 kg", true},
		{"arándanos", "Arándanos enteros ultracongelados paquete", "500 g", false},
		{"arándanos", "Arándanos vaso", "", true},
		{"Leche semidesnatada 1L", "Leche entera Asturiana 1 L", "1 L", false},
		{"Leche semidesnatada 1L", "Leche semidesnatada Asturiana 1 L", "1 L", true},
		{"Yogur griego desnatado", "Yogur griego natural Hacendado", "", false},
		{"Pan de molde blanco", "Pan de molde integral Hacendado", "", false},
		{"Yogur griego", "Yogur griego light natural", "", true},
		{"Leche semidesnatada 1L", "Leche semidesnatada Hacendado", "1 L", true},
		{"Leche semidesnatada 1L", "Leche Asturiana 1l semidesnatada", "1 L", true},
		{"Leche entera 1L", "Leche entera Asturiana 1 L", "1 L", true},
		{"Pan de molde blanco", "Pan de molde blanco Hacendado", "600 g", true},
		{"Pan de molde blanco", "Pan de molde sin corteza Alipende 450g", "450 g", true},
		{"copos de avena suaves", "Copos de avena Brüggen", "500 g", true},
		{"copos de avena suaves", "Copos avena integrales sin gluten bio Ecocesta 500g", "500 g", false},
		{"Kéfir natural", "Kéfir natural sabor suave", "1 kg", true},
	}
	for _, c := range casos {
		got := Similarity(c.nombre, c.producto, c.formato)
		if (got >= AutoThreshold) != c.quiere {
			t.Errorf("Similarity(%q, %q, %q) = %.2f (aceptado=%v), quiero aceptado=%v",
				c.nombre, c.producto, c.formato, got, got >= AutoThreshold, c.quiere)
		}
	}
}

// TestHeadMismatch documenta la regla con la que se decide.
func TestHeadMismatch(t *testing.T) {
	casos := []struct {
		query, name string
		mismatch    bool
	}{
		{"fresas", "Mermelada de fresa", true},
		{"fresas", "Fresas congeladas", false},
		{"pipas de calabaza", "Pan con pipas de calabaza", true},
		{"pipas de calabaza", "Pipas calabaza", false},
		{"Leche entera 1L", "Leche entera Hacendado", false},
	}
	for _, c := range casos {
		if got := headMismatch(Tokens(c.query), Tokens(c.name)); got != c.mismatch {
			t.Errorf("headMismatch(%q, %q) = %v, want %v", c.query, c.name, got, c.mismatch)
		}
	}
}

// TestSameToken cubre la tolerancia morfológica: el lematizador deja
// «congelada» y «ultracongelada» como palabras distintas, pero son la misma.
func TestSameToken(t *testing.T) {
	casos := []struct {
		query, name string
		want        bool
	}{
		{"alcachofa congelada", "alcachofa ultracongelada", true},
		{"fresas", "fresa", true},
		{"tomate", "tomates", true},
		{"pipas de calabaza", "calabaza", false},
		{"pipas", "pimientos", false},
		{"desnatada", "entera", false},
	}
	for _, c := range casos {
		got := sameToken(tokenOf(c.query), tokenOf(c.name))
		if got != c.want {
			t.Errorf("sameToken(%q, %q) = %v, want %v", c.query, c.name, got, c.want)
		}
	}
}

// TestVariantMismatch documenta qué palabras se consideran otro producto.
func TestVariantMismatch(t *testing.T) {
	casos := []struct {
		query, name string
		want        string // "" si son la misma variante
	}{
		{"arándanos", "Arándanos enteros ultracongelados paquete", "es ultracongelado y no se pidió"},
		{"arándanos", "Arándanos vaso", ""},
		{"Leche desnatada 1L", "Leche entera Asturiana 1 L", "se pidió desnatada y es entera"},
		{"Leche semidesnatada 1L", "Leche semidesnatada Asturiana 1 L", ""},
		{"alcachofa congelada", "Alcachofa ultracongelada", ""},
		{"Pan de molde blanco", "Pan de molde integral", "es integral y no se pidió"},
		{"Yogur griego", "Yogur griego light", ""},
		{"Yogur griego light", "Yogur griego bio", "se pidió light"},
	}
	for _, c := range casos {
		if got := variantMismatch(nameTokens(c.query), nameTokens(c.name)); got != c.want {
			t.Errorf("variantMismatch(%q, %q) = %q, want %q", c.query, c.name, got, c.want)
		}
	}
}

// TestEvaluarExplicaElMotivo es lo que usa el comando explain para enseñar por
// qué se acepta o se rechaza cada candidato.
func TestEvaluarExplicaElMotivo(t *testing.T) {
	casos := []struct {
		nombre, query, producto, formato string
		contiene                         string
	}{
		{"sustantivo ajeno", "fresas", "Mermelada de fresa Hacendado", "340 g", "sustantivo"},
		{"falta el sustantivo pedido", "pipas de calabaza", "Calabaza 1,6 Kg aprox.", "1,6 kg", "sustantivo"},
		{"variante distinta", "arándanos", "Arándanos enteros ultracongelados", "500 g", "variante distinta"},
		{"formato distinto", "Leche entera 1L", "Leche entera pack 6 x 1 L", "6 x 1 L", "formato distinto"},
		{"cobertura baja", "Aceite de oliva virgen extra", "Aceite de girasol para freír 1 L", "1 L", "cobertura baja"},
	}
	for _, c := range casos {
		v := Evaluar(c.query, c.producto, c.formato)
		if v.Aceptado {
			t.Errorf("%s: %q se ha aceptado, no debería", c.nombre, c.producto)
		}
		found := false
		for _, m := range v.Motivos {
			if strings.Contains(m, c.contiene) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: ningún motivo contiene %q, motivos: %v", c.nombre, c.contiene, v.Motivos)
		}
	}

	bueno := Evaluar("Leche semidesnatada 1L", "Leche semidesnatada Hacendado", "1 L")
	if !bueno.Aceptado || len(bueno.Motivos) != 0 {
		t.Errorf("una coincidencia buena no debe traer motivos: %+v", bueno)
	}
}

// tokenOf devuelve el primer token de un texto, ya normalizado.
func tokenOf(s string) string {
	t := Tokens(s)
	if len(t) == 0 {
		return ""
	}
	return t[0]
}
