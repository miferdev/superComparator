package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"net/http/httptest"
)

func ponerPrecio(t *testing.T, srv *httptest.Server, cuerpo string) map[string]any {
	t.Helper()
	code, body := pedir(t, srv, http.MethodPut, "/api/precios-manuales", cuerpo)
	if code != http.StatusOK {
		t.Fatalf("PUT precio manual = %d, cuerpo %v", code, body)
	}
	return body
}

// GET /api/precios-manuales con dos productos puestos a mano: salen los dos, con
// su nombre, para que la web no tenga que pedirlos uno a uno.
func TestPreciosManualesListanLosDos(t *testing.T) {
	srv := nuevoServidorCompra(t)

	ponerPrecio(t, srv, fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"precio":2.75,"nota":"oferta"}`, urlFresas))
	ponerPrecio(t, srv, fmt.Sprintf(`{"cadena":"alcampo","url":%q,"precio":1.25,"nota":"precio del lineal"}`, urlKin))

	code, body := pedirJSON(t, srv, http.MethodGet, "/api/precios-manuales", "")
	if code != http.StatusOK {
		t.Fatalf("precios-manuales = %d", code)
	}
	precios, ok := body.([]any)
	if !ok {
		t.Fatalf("la respuesta no es una lista: %T (%v)", body, body)
	}
	if len(precios) != 2 {
		t.Fatalf("precios = %d, quería 2", len(precios))
	}
	nombres := map[string]float64{}
	for _, p := range precios {
		m := p.(map[string]any)
		nombre, _ := m["nombre"].(string)
		nombres[nombre], _ = m["precio"].(float64)
	}
	if nombres["Fresas"] != 2.75 {
		t.Errorf("precio de las fresas = %v, quería 2.75", nombres["Fresas"])
	}
	if nombres["Kin enjuague bucal diario 2 x 500 ml"] != 1.25 {
		t.Errorf("precio del Kin = %v, quería 1.25", nombres["Kin enjuague bucal diario 2 x 500 ml"])
	}
}

func TestGetUnPrecioManualYSuAusencia(t *testing.T) {
	srv := nuevoServidorCompra(t)
	ponerPrecio(t, srv, fmt.Sprintf(`{"cadena":"alcampo","url":%q,"precio":1.25,"nota":"precio del lineal"}`, urlKin))

	// Con cadena y url sale solo ese, ya con el nombre resuelto.
	code, body := get(t, srv, "/api/precios-manuales?cadena=alcampo&url="+url.QueryEscape(urlKin))
	if code != http.StatusOK {
		t.Fatalf("GET uno = %d, cuerpo %v", code, body)
	}
	if body["nombre"] != "Kin enjuague bucal diario 2 x 500 ml" || body["precio"].(float64) != 1.25 {
		t.Errorf("precio manual = %v", body)
	}

	// Y uno que no tiene precio a mano es un 404, no un objeto vacío: si no lo
	// hay, no se inventa.
	code, body = get(t, srv, "/api/precios-manuales?cadena=ahorramas&url="+url.QueryEscape(urlFresas))
	if code != http.StatusNotFound {
		t.Errorf("precio inexistente = %d, quería 404 (cuerpo %v)", code, body)
	}
	if code >= 500 {
		t.Errorf("status = %d: no debería ser un fallo del servidor", code)
	}
}

func TestBorrarUnPrecioManual(t *testing.T) {
	srv := nuevoServidorCompra(t)
	ponerPrecio(t, srv, fmt.Sprintf(`{"cadena":"alcampo","url":%q,"precio":1.25}`, urlKin))

	code, body := pedir(t, srv, http.MethodDelete,
		"/api/precios-manuales?cadena=alcampo&url="+url.QueryEscape(urlKin), "")
	if code != http.StatusOK {
		t.Fatalf("DELETE = %d, cuerpo %v", code, body)
	}

	_, precios := pedirJSON(t, srv, http.MethodGet, "/api/precios-manuales", "")
	if lista, ok := precios.([]any); !ok || len(lista) != 0 {
		t.Errorf("precios = %v, quería la lista vacía", precios)
	}

	// Borrarlo otra vez no rompe nada: el DELETE es idempotente.
	code, _ = pedir(t, srv, http.MethodDelete,
		"/api/precios-manuales?cadena=alcampo&url="+url.QueryEscape(urlKin), "")
	if code != http.StatusOK {
		t.Errorf("borrar dos veces = %d, quería 200", code)
	}
}

// Cada método, su propio patrón: un DELETE no puede servirse con el patrón del
// GET, así que GET /api/precios-manuales no tiene que contestar a un borrado.
func TestCadaMetodoEsSuPatron(t *testing.T) {
	srv := nuevoServidorCompra(t)
	ponerPrecio(t, srv, fmt.Sprintf(`{"cadena":"alcampo","url":%q,"precio":1.25}`, urlKin))

	// Un POST no lo sirve ninguna de las rutas nuevas: el patrón de cada método es
	// el suyo y aquí no hay POST. Como la raíz está registrada sin método, la
	// respuesta es su "no encontrado" y no un 405.
	code, body := pedir(t, srv, http.MethodPost, "/api/precios-manuales", `{}`)
	if code == http.StatusOK {
		t.Errorf("POST /api/precios-manuales = %d, no debería servirse", code)
	}
	if code >= 500 {
		t.Errorf("POST = %d, no debería ser un fallo del servidor", code)
	}
	if _, ok := body["error"].(string); !ok {
		t.Errorf("POST = %d sin mensaje de error: %v", code, body)
	}

	// Y el GET no borra: sigue el precio de antes.
	code, lista := pedirJSON(t, srv, http.MethodGet, "/api/precios-manuales", "")
	if code != http.StatusOK {
		t.Errorf("GET = %d", code)
	}
	if precios, ok := lista.([]any); !ok || len(precios) != 1 {
		t.Errorf("precios = %v, quería que el GET no hubiera borrado nada", lista)
	}
}

func TestLosErroresDeLosPreciosManualesNoSon500(t *testing.T) {
	srv := nuevoServidorCompra(t)

	casos := []struct {
		nombre    string
		cuerpo    string
		quiere    int
		enMensaje string
	}{
		{
			nombre:    "producto que no existe",
			cuerpo:    `{"cadena":"ahorramas","url":"https://www.ahorramas.com/nada-9.html","precio":1}`,
			quiere:    http.StatusNotFound,
			enMensaje: "no está en el catálogo",
		},
		{
			nombre: "sin cadena",
			cuerpo: fmt.Sprintf(`{"url":%q,"precio":1}`, urlFresas),
			quiere: http.StatusBadRequest,
		},
		{
			nombre:    "medida sin precio por medida",
			cuerpo:    fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"precio":1,"medida":"kg"}`, urlFresas),
			quiere:    http.StatusBadRequest,
			enMensaje: "precio por medida",
		},
		{
			nombre:    "medida que no es kg ni l",
			cuerpo:    fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"precio":0,"precioMedida":3,"medida":"bolsa"}`, urlFresas),
			quiere:    http.StatusBadRequest,
			enMensaje: "kg o l",
		},
		{
			nombre:    "sin ningún precio",
			cuerpo:    fmt.Sprintf(`{"cadena":"ahorramas","url":%q,"precio":0,"precioMedida":0}`, urlFresas),
			quiere:    http.StatusBadRequest,
			enMensaje: "precio",
		},
		{
			nombre: "cuerpo que no es JSON",
			cuerpo: `precio = 1`,
			quiere: http.StatusBadRequest,
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			code, body := pedir(t, srv, http.MethodPut, "/api/precios-manuales", c.cuerpo)
			if code != c.quiere {
				t.Errorf("status = %d, quería %d (cuerpo %v)", code, c.quiere, body)
			}
			if code >= 500 {
				t.Errorf("status = %d: un precio mal escrito no es un fallo del servidor", code)
			}
			if c.enMensaje != "" {
				msg, _ := body["error"].(string)
				if !strings.Contains(msg, c.enMensaje) {
					t.Errorf("error = %q, quería que contuviera %q", msg, c.enMensaje)
				}
			}
		})
	}

	// Nada de eso ha dejado un precio a medias guardado: la lista sigue vacía.
	_, precios := pedirJSON(t, srv, http.MethodGet, "/api/precios-manuales", "")
	if lista, ok := precios.([]any); !ok || len(lista) != 0 {
		t.Errorf("precios = %v, quería la lista vacía", precios)
	}

	// Y borrar sin decir de qué producto es un 400, no un borrado masivo.
	code, body := pedir(t, srv, http.MethodDelete, "/api/precios-manuales", "")
	if code != http.StatusBadRequest {
		t.Errorf("DELETE sin parámetros = %d, quería 400", code)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "cadena") {
		t.Errorf("error = %q", msg)
	}
}
