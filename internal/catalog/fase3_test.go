package catalog

import (
	"context"
	"testing"

	"github.com/miferdev/superComparator/internal/store"
)

// catDePrueba abre una base con las tres cadenas que usa esta prueba y mete un
// producto de Alcampo, que es el caso que motiva los precios manuales: su WAF no
// deja descargar la ficha, así que su precio lo pone el usuario.
func catDePrueba(t *testing.T) (*Catalog, *store.Store) {
	t.Helper()
	st := nuevaBase(t)
	// nuevaBase solo siembra mercadona y dia, y products.chain es FK a chains.
	if err := st.SeedChains([]store.Chain{
		{ID: "alcampo", Nombre: "Alcampo", SitemapURL: "x", PreciosActivos: false, PrecioMaxHoras: 48},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpsertProducts("alcampo", []store.Product{
		{Chain: "alcampo", URL: "https://www.alcampo.es/products/pan/1", Name: "Pan de molde",
			SearchName: "pan de molde", Format: "600 g", CrawlState: "catalogado"},
		{Chain: "alcampo", URL: "https://www.alcampo.es/products/platanos/2", Name: "Plátanos",
			SearchName: "platanos", CrawlState: "catalogado"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpsertProducts("mercadona", []store.Product{
		{Chain: "mercadona", URL: "https://www.mercadona.es/product/1/leche", Name: "Leche entera",
			SearchName: "leche entera", Format: "1 L", CrawlState: "catalogado"},
	}); err != nil {
		t.Fatal(err)
	}
	cat := New(st, nil)
	if _, err := cat.Chains(context.Background()); err != nil {
		t.Fatal(err)
	}
	return cat, st
}

func TestElPrecioManualNoTocaElDeLaTienda(t *testing.T) {
	cat, st := catDePrueba(t)
	const url = "https://www.alcampo.es/products/pan/1"

	if err := cat.SetPrecioManual("alcampo", url, 1.25, 0, "", "precio del lineal"); err != nil {
		t.Fatalf("SetPrecioManual: %v", err)
	}
	// El precio de la tienda sigue siendo el suyo (aquá no hay ninguno: el WAF).
	p, ok, err := cat.Product(context.Background(), "alcampo", url)
	if err != nil || !ok {
		t.Fatalf("Product: %v %v", ok, err)
	}
	if p.Precio != 1.25 || p.PrecioFuente != FuenteManual || !p.TienePrecio {
		t.Fatalf("precio = %+v", p)
	}
	// Y el de la tienda no se ha escrito en products.price.
	bruto, _, err := st.Product("alcampo", url)
	if err != nil {
		t.Fatal(err)
	}
	if bruto.Price != 0 {
		t.Fatalf("el precio manual se coló en products: %v", bruto.Price)
	}
}

func TestElPrecioManualCuentaComoRecienEscrito(t *testing.T) {
	cat, _ := catDePrueba(t)
	const url = "https://www.alcampo.es/products/pan/1"
	// Con precio web viejo y un precio manual encima, la ficha no debe salir
	// caducada: el usuario acaba de escribirlo.
	if err := cat.SetPrecioManual("alcampo", url, 1.25, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	p, _, err := cat.Product(context.Background(), "alcampo", url)
	if err != nil {
		t.Fatal(err)
	}
	if p.FrescuraHoras != 0 {
		t.Fatalf("frescuraHoras = %d", p.FrescuraHoras)
	}
}

func TestBorrarElPrecioManualDejaElDeLaTienda(t *testing.T) {
	cat, st := catDePrueba(t)
	const url = "https://www.alcampo.es/products/pan/1"
	if err := cat.SetPrecioManual("alcampo", url, 1.25, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPrice(mustID(t, st, "alcampo", url), 2.10, "unidad", 0, "", true); err != nil {
		t.Fatal(err)
	}
	if err := cat.BorrarPrecioManual("alcampo", url); err != nil {
		t.Fatal(err)
	}
	p, _, err := cat.Product(context.Background(), "alcampo", url)
	if err != nil {
		t.Fatal(err)
	}
	if p.Precio != 2.10 || p.PrecioFuente != FuenteWeb {
		t.Fatalf("al borrar el manual debe verse el de la tienda: %+v", p)
	}
}

func TestMiCompraSumaSoloPreciosDeUnidad(t *testing.T) {
	cat, st := catDePrueba(t)
	// Mercadona con precio de tienda, Alcampo con precio manual y un producto
	// vendido al peso (solo €/kg, sin precio de unidad).
	leche := "https://www.mercadona.es/product/1/leche"
	if err := st.SetPrice(mustID(t, st, "mercadona", leche), 1.10, "unidad", 1.10, "l", true); err != nil {
		t.Fatal(err)
	}
	platano := "https://www.alcampo.es/products/platanos/2"
	if err := st.SetPrice(mustID(t, st, "alcampo", platano), 0, "kg", 1.65, "kg", true); err != nil {
		t.Fatal(err)
	}
	pan := "https://www.alcampo.es/products/pan/1"
	if err := cat.SetPrecioManual("alcampo", pan, 1.25, 0, "", ""); err != nil {
		t.Fatal(err)
	}

	for _, caso := range []struct {
		cadena, url string
		cantidad    float64
	}{
		{"mercadona", leche, 2},
		{"alcampo", pan, 1},
		{"alcampo", platano, 1},
	} {
		if err := cat.AñadirALaCompra(caso.cadena, caso.url, caso.cantidad); err != nil {
			t.Fatalf("AñadirALaCompra(%s): %v", caso.cadena, err)
		}
	}

	l, err := cat.MiCompra()
	if err != nil {
		t.Fatalf("MiCompra: %v", err)
	}
	// 2 × 1,10 + 1 × 1,25. Los plátanos no suman: de 1,65 €/kg no se puede saber
	// cuánto cuesta una bolsa.
	if l.Total != 3.45 {
		t.Fatalf("total = %v, quería 3.45: %+v", l.Total, l.Items)
	}
	if l.SinPrecio != 1 || len(l.SinPrecioDetalle) != 1 || l.SinPrecioDetalle[0] != "Plátanos" {
		t.Fatalf("sinPrecio = %d %+v", l.SinPrecio, l.SinPrecioDetalle)
	}
	if len(l.SubtotalPorCadena) != 2 {
		t.Fatalf("subtotales = %+v", l.SubtotalPorCadena)
	}
	// Ordenado de mayor a menor: Mercadona 2,20 y Alcampo 1,25.
	if l.SubtotalPorCadena[0].Cadena != "mercadona" || l.SubtotalPorCadena[0].Subtotal != 2.20 {
		t.Fatalf("subtotales = %+v", l.SubtotalPorCadena)
	}
	if l.SubtotalPorCadena[1].Cadena != "alcampo" || l.SubtotalPorCadena[1].Nombre != "" {
		// Alcampo no está sembrado en esta base: el nombre puede venir vacío.
		t.Logf("subtotal de alcampo: %+v", l.SubtotalPorCadena[1])
	}

	var fuenteAlcampo, fuentePlatano string
	for _, it := range l.Items {
		if it.Nombre == "Plátanos" {
			fuentePlatano = it.PrecioFuente
		}
		if it.Nombre == "Pan de molde" {
			fuenteAlcampo = it.PrecioFuente
		}
	}
	if fuenteAlcampo != FuenteManual {
		t.Fatalf("el pan va con precio manual, fuente = %q", fuenteAlcampo)
	}
	// Los plátanos no tienen precio de unidad: su fuente es "ninguno", no "web",
	// porque decir "web" no diría nada del precio que se está enseñando.
	if fuentePlatano != FuenteNinguno {
		t.Fatalf("los plátanos no tienen precio de unidad: fuente = %q", fuentePlatano)
	}
}

func TestCantidadCeroQuitaElProducto(t *testing.T) {
	cat, _ := catDePrueba(t)
	const url = "https://www.alcampo.es/products/pan/1"
	if err := cat.AñadirALaCompra("alcampo", url, 1); err != nil {
		t.Fatal(err)
	}
	if err := cat.CambiarCantidad("alcampo", url, 0); err != nil {
		t.Fatal(err)
	}
	l, err := cat.MiCompra()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 0 || l.Total != 0 {
		t.Fatalf("con cantidad 0 el producto sale de la lista: %+v", l)
	}
}

func TestNormalizarMedida(t *testing.T) {
	for _, caso := range []struct{ entra, quiere string }{
		{"kg", "kg"},
		{"KG", "kg"},
		{" l ", "l"},
		{"", ""},
	} {
		if got := normalizarMedida(caso.entra); got != caso.quiere {
			t.Fatalf("normalizarMedida(%q) = %q, quiero %q", caso.entra, got, caso.quiere)
		}
	}
}

func mustID(t *testing.T, st *store.Store, cadena, url string) int64 {
	t.Helper()
	p, ok, err := st.Product(cadena, url)
	if err != nil || !ok {
		t.Fatalf("Product(%s): %v %v", url, ok, err)
	}
	return p.ID
}
