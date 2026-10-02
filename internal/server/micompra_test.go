package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miferdev/superComparator/internal/catalog"
	"github.com/miferdev/superComparator/internal/store"
)

const (
	urlFresas  = "https://www.ahorramas.com/fresas-250g-1.html"
	urlPlatano = "https://www.ahorramas.com/platano-canarias-1kg-3.html"
	urlKin     = "https://www.compraonline.alcampo.es/products/kin-enjuague-bucal-diario-con-acción-antiplaca-kin-2-x-500-ml/894645"
)

// nuevoServidorCompra abre una base con los tres productos que necesita esta
// parte de la API:
//
//   - Fresas, con precio de unidad (2,50 €): es lo que se suma al total.
//   - Plátanos, vendidos solo por kilos (3,50 €/kg y sin precio de unidad): es el
//     caso que no se puede sumar y por el que existe SinPrecio.
//   - Kin de Alcampo, sin ningún precio: su WAF no deja descargar la ficha, así
//     que el precio lo pone el usuario.
func nuevoServidorCompra(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "compra.db"))
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
		{Chain: "ahorramas", URL: urlFresas, SKU: "1", Name: "Fresas", SearchName: "fresas",
			Format: "250 g", MeasureValue: 0.25, MeasureUnit: "kg", CrawlState: "catalogado"},
		{Chain: "ahorramas", URL: urlPlatano, SKU: "3", Name: "Plátanos", SearchName: "platano canarias",
			Format: "1 kg", MeasureValue: 1, MeasureUnit: "kg", CrawlState: "catalogado"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.UpsertProducts("alcampo", []store.Product{{
		Chain: "alcampo", URL: urlKin, SKU: "894645", Name: "Kin enjuague bucal diario 2 x 500 ml",
		SearchName: "kin enjuague bucal diario", Format: "2 x 500 ml", MeasureValue: 1,
		MeasureUnit: "l", CrawlState: "catalogado"}})
	if err != nil {
		t.Fatal(err)
	}

	fresas, ok, err := st.Product("ahorramas", urlFresas)
	if err != nil || !ok {
		t.Fatalf("fresas no encontrada: %v", err)
	}
	if err := st.SetPrice(fresas.ID, 2.50, "unidad", 10, "kg", true); err != nil {
		t.Fatal(err)
	}
	// El plátano solo se publica por kilos: su precio de unidad se queda a 0 a
	// propósito, que es como lo guarda la cola de precios.
	platano, ok, err := st.Product("ahorramas", urlPlatano)
	if err != nil || !ok {
		t.Fatalf("plátanos no encontrados: %v", err)
	}
	if err := st.SetPrice(platano.ID, 0, "medida", 3.50, "kg", true); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(catalog.New(st, nil), st, nil, "127.0.0.1:0").Handler())
	t.Cleanup(srv.Close)
	return srv
}

// pedir manda una petición con cuerpo y devuelve el status y el JSON que salen.
// Las que responden con un objeto.
func pedir(t *testing.T, srv *httptest.Server, method, path, cuerpo string) (int, map[string]any) {
	t.Helper()
	code, raw := pedirRaw(t, srv, method, path, cuerpo)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return code, body
}

// pedirJSON es el mismo para las que responden con una lista, como
// /api/precios-manuales: get() de server_test.go solo vale para objetos.
func pedirJSON(t *testing.T, srv *httptest.Server, method, path, cuerpo string) (int, any) {
	t.Helper()
	code, raw := pedirRaw(t, srv, method, path, cuerpo)
	var body any
	_ = json.Unmarshal(raw, &body)
	return code, body
}

func pedirRaw(t *testing.T, srv *httptest.Server, method, path, cuerpo string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(cuerpo))
	if err != nil {
		t.Fatal(err)
	}
	if cuerpo != "" {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

// añadir mete un producto en la lista por la API y falla el test si no sale un 201.
func añadir(t *testing.T, srv *httptest.Server, cadena, productURL string, cantidad float64) map[string]any {
	t.Helper()
	code, body := pedir(t, srv, http.MethodPost, "/api/mi-compra",
		fmt.Sprintf(`{"cadena":%q,"url":%q,"cantidad":%g}`, cadena, productURL, cantidad))
	if code != http.StatusCreated {
		t.Fatalf("añadir %s = %d, cuerpo %v", productURL, code, body)
	}
	return body
}

func primeraLinea(t *testing.T, compra map[string]any) map[string]any {
	t.Helper()
	items, ok := compra["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %v, quería una línea", compra["items"])
	}
	return items[0].(map[string]any)
}

func TestMiCompraVacia(t *testing.T) {
	srv := nuevoServidorCompra(t)
	code, body := get(t, srv, "/api/mi-compra")
	if code != http.StatusOK {
		t.Fatalf("mi-compra = %d", code)
	}
	// Una lista vacía es `[]` y no `null`: la web la recorre sin mirar el tipo.
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items = %v, quería una lista vacía", body["items"])
	}
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v, quería 0", body["total"])
	}
	if body["sinPrecio"].(float64) != 0 {
		t.Errorf("sinPrecio = %v, quería 0", body["sinPrecio"])
	}
	// Sin líneas sin precio no hay nada que avisar: el aviso vacío no mete ruido.
	if body["avisoTotal"] != "" {
		t.Errorf("avisoTotal = %v, quería vacío", body["avisoTotal"])
	}
}

func TestMiCompraSumaElSubtotalAlTotal(t *testing.T) {
	srv := nuevoServidorCompra(t)
	añadir(t, srv, "ahorramas", urlFresas, 1)

	code, body := get(t, srv, "/api/mi-compra")
	if code != http.StatusOK {
		t.Fatalf("mi-compra = %d", code)
	}
	linea := primeraLinea(t, body)
	if linea["nombre"] != "Fresas" {
		t.Errorf("nombre = %v", linea["nombre"])
	}
	if linea["subtotal"].(float64) != 2.50 {
		t.Errorf("subtotal = %v, quería 2.50", linea["subtotal"])
	}
	if body["total"].(float64) != 2.50 {
		t.Errorf("total = %v, quería 2.50", body["total"])
	}
	if linea["precioFuente"] != catalog.FuenteWeb {
		t.Errorf("precioFuente = %v, quería %q", linea["precioFuente"], catalog.FuenteWeb)
	}
	subtotales, ok := body["subtotalPorCadena"].([]any)
	if !ok || len(subtotales) != 1 {
		t.Fatalf("subtotalPorCadena = %v", body["subtotalPorCadena"])
	}
	if s := subtotales[0].(map[string]any); s["cadena"] != "ahorramas" || s["subtotal"].(float64) != 2.50 {
		t.Errorf("subtotal de ahorramas = %v", s)
	}
}

// TestLoVendidoAlPesoNoSumaElTotal es el test que fija la regla que más importa:
// un producto que solo se publica por kilos no tiene precio de unidad, y de eso
// no se puede sacar cuánto cuesta una bolsa. El total no se toca y la línea no
// desaparece: se cuenta aparte y se explica.
func TestLoVendidoAlPesoNoSumaElTotal(t *testing.T) {
	srv := nuevoServidorCompra(t)

	añadir(t, srv, "ahorramas", urlPlatano, 2)
	_, body := get(t, srv, "/api/mi-compra")
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v con solo lo vendido al peso, quería 0", body["total"])
	}
	linea := primeraLinea(t, body)
	if linea["subtotal"].(float64) != 0 {
		t.Errorf("subtotal = %v, quería 0", linea["subtotal"])
	}
	if !linea["precioSoloMedida"].(bool) {
		t.Errorf("precioSoloMedida = %v, quería true", linea["precioSoloMedida"])
	}
	if linea["precioFuente"] != catalog.FuenteNinguno {
		t.Errorf("precioFuente = %v, quería %q", linea["precioFuente"], catalog.FuenteNinguno)
	}
	// El €/kg viene, para que la web pueda enseñarlo, pero no se transforma en precio.
	if linea["precioMedida"].(float64) != 3.50 || linea["medida"] != "kg" {
		t.Errorf("precio por medida = %v %v", linea["precioMedida"], linea["medida"])
	}
	if body["sinPrecio"].(float64) != 1 {
		t.Errorf("sinPrecio = %v, quería 1", body["sinPrecio"])
	}
	detalle, ok := body["sinPrecioDetalle"].([]any)
	if !ok || len(detalle) != 1 || detalle[0] != "Plátanos" {
		t.Errorf("sinPrecioDetalle = %v", body["sinPrecioDetalle"])
	}
	aviso, _ := body["avisoTotal"].(string)
	if aviso == "" {
		t.Error("avisoTotal vacío: el total parecería completo cuando no lo está")
	}

	// Y con las dos cosas en la lista, el plátano sigue sin mover el total.
	añadir(t, srv, "ahorramas", urlFresas, 1)
	_, body = get(t, srv, "/api/mi-compra")
	if body["total"].(float64) != 2.50 {
		t.Errorf("total = %v, quería 2.50 (solo las fresas)", body["total"])
	}
	if body["sinPrecio"].(float64) != 1 {
		t.Errorf("sinPrecio = %v, quería 1", body["sinPrecio"])
	}
}

func TestCantidadMultiplicaElSubtotalYElTotal(t *testing.T) {
	srv := nuevoServidorCompra(t)
	añadir(t, srv, "ahorramas", urlFresas, 1)

	code, body := pedir(t, srv, http.MethodPut, "/api/mi-compra",
		fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"cantidad":2}`, urlFresas))
	if code != http.StatusOK {
		t.Fatalf("PUT = %d, cuerpo %v", code, body)
	}
	if linea := primeraLinea(t, body); linea["subtotal"].(float64) != 5.00 {
		t.Errorf("subtotal = %v, quería 5.00", linea["subtotal"])
	}
	if body["total"].(float64) != 5.00 {
		t.Errorf("total = %v, quería 5.00", body["total"])
	}
}

// Cantidad 0 quita el producto: es lo que quiere decir el usuario cuando pone
// cero, y no es un 400.
func TestCantidadCeroQuitaElProducto(t *testing.T) {
	srv := nuevoServidorCompra(t)
	añadir(t, srv, "ahorramas", urlFresas, 2)

	code, body := pedir(t, srv, http.MethodPut, "/api/mi-compra",
		fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"cantidad":0}`, urlFresas))
	if code != http.StatusOK {
		t.Fatalf("PUT cantidad 0 = %d, cuerpo %v", code, body)
	}
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items = %v, quería que el producto desaparecería", body["items"])
	}
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v, quería 0", body["total"])
	}
}

func TestQuitarUnProductoYVaciarLaLista(t *testing.T) {
	srv := nuevoServidorCompra(t)
	añadir(t, srv, "ahorramas", urlFresas, 1)
	añadir(t, srv, "ahorramas", urlPlatano, 1)

	// Quitar uno.
	code, body := pedir(t, srv, http.MethodDelete,
		"/api/mi-compra?cadena=ahorramas&url="+url.QueryEscape(urlPlatano), "")
	if code != http.StatusOK {
		t.Fatalf("DELETE uno = %d, cuerpo %v", code, body)
	}
	_, compra := get(t, srv, "/api/mi-compra")
	if items, ok := compra["items"].([]any); !ok || len(items) != 1 {
		t.Fatalf("items = %v, quería solo las fresas", compra["items"])
	}

	// Y vaciarlo todo, sin parámetros.
	code, body = pedir(t, srv, http.MethodDelete, "/api/mi-compra", "")
	if code != http.StatusOK {
		t.Fatalf("DELETE sin parámetros = %d, cuerpo %v", code, body)
	}
	_, compra = get(t, srv, "/api/mi-compra")
	if items, ok := compra["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items = %v, quería la lista vacía", compra["items"])
	}
	if compra["total"].(float64) != 0 {
		t.Errorf("total = %v, quería 0", compra["total"])
	}
	if compra["sinPrecio"].(float64) != 0 {
		t.Errorf("sinPrecio = %v, quería 0", compra["sinPrecio"])
	}
}

// Un precio puesto a mano manda en el subtotal de su línea, y la línea dice de
// dónde sale: si no, un total que mezcla las dos fuentes no se puede leer.
func TestElPrecioManualMandaEnSuSubtotal(t *testing.T) {
	srv := nuevoServidorCompra(t)
	code, _ := pedir(t, srv, http.MethodPut, "/api/precios-manuales",
		fmt.Sprintf(`{"cadena":"alcampo","url":%q,"precio":1.25,"precioMedida":0,"medida":"","nota":"precio del lineal"}`, urlKin))
	if code != http.StatusOK {
		t.Fatalf("PUT precio manual = %d", code)
	}

	añadir(t, srv, "alcampo", urlKin, 2)
	_, body := get(t, srv, "/api/mi-compra")
	linea := primeraLinea(t, body)
	if linea["precioFuente"] != catalog.FuenteManual {
		t.Errorf("precioFuente = %v, quería %q", linea["precioFuente"], catalog.FuenteManual)
	}
	if linea["precio"].(float64) != 1.25 {
		t.Errorf("precio = %v, quería 1.25", linea["precio"])
	}
	if linea["subtotal"].(float64) != 2.50 {
		t.Errorf("subtotal = %v, quería 2.50", linea["subtotal"])
	}
	if body["total"].(float64) != 2.50 {
		t.Errorf("total = %v, quería 2.50", body["total"])
	}
	// Con precio, la línea se suma: no cuenta como línea sin precio.
	if body["sinPrecio"].(float64) != 0 {
		t.Errorf("sinPrecio = %v, quería 0", body["sinPrecio"])
	}
}

// La medida se normaliza en minúsculas ("KG" y "kg" son lo mismo), también
// cuando el precio viene puesto a mano.
func TestElPrecioMedidaManualSeNormaliza(t *testing.T) {
	srv := nuevoServidorCompra(t)
	code, dto := pedir(t, srv, http.MethodPut, "/api/precios-manuales",
		fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"precio":0,"precioMedida":3.99,"medida":"KG","nota":"del lineal"}`, urlPlatano))
	if code != http.StatusOK {
		t.Fatalf("PUT precio por medida = %d, cuerpo %v", code, dto)
	}
	if dto["medida"] != "kg" {
		t.Errorf("medida en la respuesta = %v, quería kg", dto["medida"])
	}

	añadir(t, srv, "ahorramas", urlPlatano, 1)
	_, body := get(t, srv, "/api/mi-compra")
	linea := primeraLinea(t, body)
	if linea["medida"] != "kg" {
		t.Errorf("medida = %v, quería kg", linea["medida"])
	}
	if linea["precioMedida"].(float64) != 3.99 {
		t.Errorf("precioMedida = %v, quería 3.99", linea["precioMedida"])
	}
	// Solo hay precio por medida: sigue sin poder sumar, y no por el 0 del unidad
	// sino porque de un €/kg no sale una bolsa.
	if body["total"].(float64) != 0 || body["sinPrecio"].(float64) != 1 {
		t.Errorf("total = %v, sinPrecio = %v; quería 0 y 1", body["total"], body["sinPrecio"])
	}
}

// Un error de la lista es un 404 o un 400 con el motivo en español, nunca un 500
// genérico: el 500 es para cuando ha fallado el servidor.
func TestLosErroresDeLaListaNoSon500(t *testing.T) {
	srv := nuevoServidorCompra(t)

	casos := []struct {
		nombre    string
		method    string
		path      string
		cuerpo    string
		quiere    int
		enMensaje string
	}{
		{
			nombre: "producto que no existe", method: http.MethodPost, path: "/api/mi-compra",
			cuerpo:    `{"cadena":"ahorramas","url":"https://www.ahorramas.com/nada-9.html","cantidad":1}`,
			quiere:    http.StatusNotFound,
			enMensaje: "no está en el catálogo",
		},
		{
			nombre: "sin url", method: http.MethodPost, path: "/api/mi-compra",
			cuerpo: `{"cadena":"ahorramas","cantidad":1}`,
			quiere: http.StatusBadRequest,
		},
		{
			nombre: "cuerpo que no es JSON", method: http.MethodPost, path: "/api/mi-compra",
			cuerpo: `esto no es json`,
			quiere: http.StatusBadRequest,
		},
		{
			nombre: "cantidad negativa", method: http.MethodPost, path: "/api/mi-compra",
			cuerpo:    fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"cantidad":-3}`, urlFresas),
			quiere:    http.StatusBadRequest,
			enMensaje: "cantidad",
		},
		{
			nombre: "producto que no está en la lista", method: http.MethodPut, path: "/api/mi-compra",
			cuerpo:    fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"cantidad":4}`, urlFresas),
			quiere:    http.StatusNotFound,
			enMensaje: "no está en la lista",
		},
		{
			nombre: "PUT sin cantidad", method: http.MethodPut, path: "/api/mi-compra",
			cuerpo: fmt.Sprintf(`{"cadena":"ahorramas","url":%q}`, urlFresas),
			quiere: http.StatusBadRequest,
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			code, body := pedir(t, srv, c.method, c.path, c.cuerpo)
			if code != c.quiere {
				t.Errorf("status = %d, quería %d (cuerpo %v)", code, c.quiere, body)
			}
			if code >= 500 {
				t.Errorf("status = %d: un error de la lista no es un fallo del servidor", code)
			}
			if c.enMensaje != "" {
				msg, _ := body["error"].(string)
				if !strings.Contains(msg, c.enMensaje) {
					t.Errorf("error = %q, quería que contuviera %q", msg, c.enMensaje)
				}
			}
		})
	}

	// El mismo producto dos veces no es un 500, y el motivo dice que se cambie la
	// cantidad en vez de añadirlo otra vez.
	añadir(t, srv, "ahorramas", urlFresas, 1)
	code, body := pedir(t, srv, http.MethodPost, "/api/mi-compra",
		fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"cantidad":1}`, urlFresas))
	if code != http.StatusBadRequest {
		t.Errorf("añadir dos veces = %d, quería 400", code)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "ya está en la lista") {
		t.Errorf("error = %q", msg)
	}
}
