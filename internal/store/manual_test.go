package store

import (
	"strings"
	"testing"
	"time"
)

// contarFilas cuenta filas de una tabla. Sirve para asertar que un precio manual
// no se ha colado en el precio del producto ni en el historial.
func contarFilas(t *testing.T, st *Store, tabla string) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM ` + tabla).Scan(&n); err != nil {
		t.Fatalf("contar %s: %v", tabla, err)
	}
	return n
}

// El precio manual vive en su tabla: el del producto y el historial no se tocan.
func TestPrecioManualNoTocaElPrecioWeb(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	p, _, err := st.Product("mercadona", "m1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if err := st.SetPrice(p.ID, 3.75, "kg", 3.75, "kg", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	historial := contarFilas(t, st, "price_history")

	if err := st.SetPrecioManual(PrecioManual{
		Chain: "mercadona", ProductURL: "m1",
		Precio: 2.1, PrecioMedida: 4.2, Medida: "kg", Nota: "precio del lineal",
	}); err != nil {
		t.Fatalf("SetPrecioManual: %v", err)
	}

	got, ok, err := st.GetPrecioManual("mercadona", "m1")
	if err != nil || !ok {
		t.Fatalf("GetPrecioManual = %v %v", ok, err)
	}
	if got.Precio != 2.1 || got.PrecioMedida != 4.2 || got.Medida != "kg" || got.Nota != "precio del lineal" {
		t.Fatalf("precio manual = %+v", got)
	}
	if got.Actualizado.IsZero() {
		t.Fatalf("precio manual sin marca de tiempo: %+v", got)
	}

	// El producto sigue con el precio que publica la tienda.
	p2, _, err := st.Product("mercadona", "m1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if p2.Price != 3.75 || p2.MeasurePrice != 3.75 {
		t.Fatalf("el precio manual pisó el del producto: %+v", p2)
	}
	if n := contarFilas(t, st, "price_history"); n != historial {
		t.Fatalf("el precio manual dejó rastro en el historial (%d filas, antes %d)", n, historial)
	}

	// Un producto de otra cadena no ve el precio de esta.
	if _, ok, err := st.GetPrecioManual("ahorramas", "m1"); err != nil || ok {
		t.Fatalf("el precio manual se lee desde otra cadena: %v %v", ok, err)
	}
	if todos, err := st.PreciosManuales(); err != nil || len(todos) != 1 {
		t.Fatalf("PreciosManuales = %+v, %v", todos, err)
	}
}

func TestSetPrecioManualUpsert(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	for i, precio := range []float64{2.1, 1.5} {
		if err := st.SetPrecioManual(PrecioManual{
			Chain: "ahorramas", ProductURL: "a1", Precio: precio,
		}); err != nil {
			t.Fatalf("SetPrecioManual %d: %v", i, err)
		}
	}
	got, ok, err := st.GetPrecioManual("ahorramas", "a1")
	if err != nil || !ok {
		t.Fatalf("GetPrecioManual = %v %v", ok, err)
	}
	if got.Precio != 1.5 {
		t.Fatalf("no se actualizó: %+v", got)
	}
	if n := contarFilas(t, st, "precios_manuales"); n != 1 {
		t.Fatalf("precios_manuales = %d filas, quiero 1", n)
	}
	// La marca es del último momento, no de cuando se creó.
	if !got.Actualizado.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("actualizado = %v", got.Actualizado)
	}

	// Solo precio por medida: es lo que se hace con lo vendido al peso, y su
	// precio de unidad se queda a 0 a propósito.
	if err := st.SetPrecioManual(PrecioManual{
		Chain: "ahorramas", ProductURL: "a1", PrecioMedida: 8.25, Medida: "KG",
	}); err != nil {
		t.Fatalf("SetPrecioManual por medida: %v", err)
	}
	got, _, _ = st.GetPrecioManual("ahorramas", "a1")
	if got.Precio != 0 || got.PrecioMedida != 8.25 || got.Medida != "kg" {
		t.Fatalf("precio por medida = %+v", got)
	}
}

func TestBorrarPrecioManual(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	p, _, err := st.Product("mercadona", "m3")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if err := st.SetPrice(p.ID, 1.15, "unidad", 1.15, "l", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	if err := st.SetPrecioManual(PrecioManual{Chain: "mercadona", ProductURL: "m3", Precio: 0.99}); err != nil {
		t.Fatalf("SetPrecioManual: %v", err)
	}

	if err := st.BorrarPrecioManual("mercadona", "m3"); err != nil {
		t.Fatalf("BorrarPrecioManual: %v", err)
	}
	if _, ok, err := st.GetPrecioManual("mercadona", "m3"); err != nil || ok {
		t.Fatalf("sigue el precio manual: %v %v", ok, err)
	}
	got, _, err := st.Product("mercadona", "m3")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if got.Price != 1.15 {
		t.Fatalf("borrar el precio manual tocó el de la tienda: %+v", got)
	}
	// Borrar dos veces no es un error: quitar un precio es idempotente.
	if err := st.BorrarPrecioManual("mercadona", "m3"); err != nil {
		t.Fatalf("BorrarPrecioManual repetido: %v", err)
	}
}

// Preferimos un error claro a guardar un precio que no significa nada: quien
// llama se tiene que enterar de que no se ha guardado.
func TestSetPrecioManualValidacion(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	casos := []struct {
		que    string
		manual PrecioManual
		queria string
	}{
		{
			que:    "producto que no está en el catálogo",
			manual: PrecioManual{Chain: "mercadona", ProductURL: "no-existe", Precio: 1},
			queria: "no está en el catálogo",
		},
		{
			que:    "precio a cero sin medida",
			manual: PrecioManual{Chain: "mercadona", ProductURL: "m1"},
			queria: "precio de unidad",
		},
		{
			que:    "precio a cero con precio por medida sin su medida",
			manual: PrecioManual{Chain: "mercadona", ProductURL: "m1", PrecioMedida: 3.5},
			queria: "medida",
		},
		{
			que:    "medida suelta sin precio de medida",
			manual: PrecioManual{Chain: "mercadona", ProductURL: "m1", Precio: 1, Medida: "kg"},
			queria: "sin un precio por medida",
		},
		{
			que:    "medida que no es kg ni l",
			manual: PrecioManual{Chain: "mercadona", ProductURL: "m1", PrecioMedida: 3.5, Medida: "ud"},
			queria: "kg o l",
		},
		{
			que:    "precio en negativo",
			manual: PrecioManual{Chain: "mercadona", ProductURL: "m1", Precio: -1},
			queria: "precio de unidad",
		},
	}
	for _, c := range casos {
		err := st.SetPrecioManual(c.manual)
		if err == nil {
			t.Fatalf("%s: se guardó sin quejarse", c.que)
		}
		if !strings.Contains(err.Error(), c.queria) {
			t.Fatalf("%s: error = %q, quiero que hable de %q", c.que, err, c.queria)
		}
	}
	if n := contarFilas(t, st, "precios_manuales"); n != 0 {
		t.Fatalf("precios_manuales = %d filas tras errores de validación", n)
	}
}
