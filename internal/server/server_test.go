package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miferdev/superComparator/internal/catalog"
	"github.com/miferdev/superComparator/internal/store"
)

func nuevoServidor(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.SeedChains([]store.Chain{
		{ID: "ahorramas", Nombre: "Ahorramas", SitemapURL: "x", PreciosActivos: true, PrecioMaxHoras: 24},
		{ID: "alcampo", Nombre: "Alcampo", SitemapURL: "x", PreciosActivos: false, PrecioMaxHoras: 24},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err = st.UpsertProducts("ahorramas", []store.Product{
		{Chain: "ahorramas", URL: "https://www.ahorramas.com/fresas-250g-1.html", SKU: "1",
			Name: "Fresas", SearchName: "fresa", Format: "250 g", MeasureValue: 0.25, MeasureUnit: "kg",
			CrawlState: "catalogado"},
		{Chain: "ahorramas", URL: "https://www.ahorramas.com/leche-entera-1l-2.html", SKU: "2",
			Name: "Leche entera", SearchName: "leche entera", Format: "1 L", MeasureValue: 1, MeasureUnit: "l",
			CrawlState: "catalogado"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// El precio nunca lo pone el indexador: lo pone la cola al visitar la ficha.
	fresas, ok, err := st.Product("ahorramas", "https://www.ahorramas.com/fresas-250g-1.html")
	if err != nil || !ok {
		t.Fatalf("fresas no encontrada: %v", err)
	}
	if err := st.SetPrice(fresas.ID, 2.50, "unidad", 10, "kg", true); err != nil {
		t.Fatal(err)
	}

	// Alcampo: catálogo sí, precio no (su WAF bloquea las fichas).
	_, _, err = st.UpsertProducts("alcampo", []store.Product{{
		Chain: "alcampo", URL: "https://www.compraonline.alcampo.es/products/kin-enjuague-bucal-diario-con-acción-antiplaca-kin-2-x-500-ml/894645",
		SKU: "894645", Name: "Kin enjuague bucal diario 2 x 500 ml", SearchName: "kin enjuague bucal diario 2 x 500 ml",
		Format: "2 x 500 ml", MeasureValue: 1, MeasureUnit: "l", CrawlState: "catalogado"}})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(catalog.New(st, nil), st, nil, "127.0.0.1:0").Handler())
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestEstado(t *testing.T) {
	srv := nuevoServidor(t)
	code, body := get(t, srv, "/api/estado")
	if code != http.StatusOK {
		t.Fatalf("estado = %d", code)
	}
	if !strings.Contains(body["version"].(string), "supercomparator") {
		t.Errorf("version = %v", body["version"])
	}
	cadenas, ok := body["cadenas"].([]any)
	if !ok || len(cadenas) != 2 {
		t.Fatalf("cadenas = %v", body["cadenas"])
	}
}

func TestCatalogoBusca(t *testing.T) {
	srv := nuevoServidor(t)
	code, body := get(t, srv, "/api/catalogo?q=fresas")
	if code != http.StatusOK {
		t.Fatalf("catalogo = %d", code)
	}
	productos := body["productos"].([]any)
	if len(productos) != 1 {
		t.Fatalf("productos = %d, want 1", len(productos))
	}
	primero := productos[0].(map[string]any)
	if primero["nombre"] != "Fresas" {
		t.Errorf("nombre = %v", primero["nombre"])
	}
	if !primero["tienePrecio"].(bool) {
		t.Error("debería traer precio")
	}
}

func TestCatalogoFiltraPorCadena(t *testing.T) {
	srv := nuevoServidor(t)
	_, body := get(t, srv, "/api/catalogo?cadena=alcampo")
	productos := body["productos"].([]any)
	if len(productos) != 1 {
		t.Fatalf("productos = %d, want 1", len(productos))
	}
	p := productos[0].(map[string]any)
	if p["nombreCadena"] != "Alcampo" {
		t.Errorf("nombreCadena = %v", p["nombreCadena"])
	}
	if p["tienePrecio"].(bool) {
		t.Error("Alcampo no debe traer precio todavía")
	}
}

func TestCatalogoOrdenaPorPrecio(t *testing.T) {
	srv := nuevoServidor(t)
	_, body := get(t, srv, "/api/catalogo?orden=precio_asc")
	productos := body["productos"].([]any)
	if len(productos) < 2 {
		t.Fatalf("productos = %d", len(productos))
	}
	primero := productos[0].(map[string]any)["precio"].(float64)
	segundo := productos[1].(map[string]any)["precio"].(float64)
	if primero > segundo {
		t.Errorf("ordenado mal: %v antes que %v", primero, segundo)
	}
}

// TestProductoConAcentosEnLaURL es el caso que puede romperse con las eñes y
// los acentos: la URL de Alcampo lleva "acción" en crudo.
func TestProductoConAcentosEnLaURL(t *testing.T) {
	srv := nuevoServidor(t)
	const raw = "https://www.compraonline.alcampo.es/products/kin-enjuague-bucal-diario-con-acción-antiplaca-kin-2-x-500-ml/894645"

	// Se manda como parámetro de query, escapado: es lo que hará la web.
	code, body := get(t, srv, "/api/producto?cadena=alcampo&url="+url.QueryEscape(raw))
	if code != http.StatusOK {
		t.Fatalf("producto = %d, cuerpo %v", code, body)
	}
	if body["nombre"] != "Kin enjuague bucal diario 2 x 500 ml" {
		t.Errorf("nombre = %v", body["nombre"])
	}
	// Y el enlace que devuelve la web debe ser exactamente el de la tienda.
	if body["url"] != raw {
		t.Errorf("url = %v, want %v", body["url"], raw)
	}

	// Sin escapar del todo, la query llega con el acento mal y no debe reventar
	// el servidor: o lo encuentra o responde 404, pero nunca un 500.
	resp, err := http.Get(srv.URL + "/api/producto?cadena=alcampo&url=" + raw)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Errorf("status = %d, no debería ser error del servidor", resp.StatusCode)
	}
}

func TestProductoNoEncontrado(t *testing.T) {
	srv := nuevoServidor(t)
	code, _ := get(t, srv, "/api/producto?cadena=mercadona&url="+url.QueryEscape("https://x/1"))
	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
	code, _ = get(t, srv, "/api/producto")
	if code != http.StatusBadRequest {
		t.Errorf("sin parámetros = %d, want 400", code)
	}
}

func TestRaizSirveHTML(t *testing.T) {
	srv := nuevoServidor(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}
