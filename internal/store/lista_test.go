package store

import (
	"strings"
	"testing"
)

// ponerPrecio deja un producto con el precio que publica la tienda. precio a 0
// con precio por medida es lo que hace una tienda con lo vendido al peso: no
// publica precio de unidad y no se inventa.
func ponerPrecio(t *testing.T, st *Store, chain, url string, precio, precioMedida float64, unidad string) {
	t.Helper()
	p, ok, err := st.Product(chain, url)
	if err != nil || !ok {
		t.Fatalf("Product(%s, %s): %v %v", chain, url, ok, err)
	}
	if err := st.SetPrice(p.ID, precio, "unidad", precioMedida, unidad, true); err != nil {
		t.Fatalf("SetPrice(%s, %s): %v", chain, url, err)
	}
}

func TestListaCompraSubtotales(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	// Con precio de la tienda, a 1,80 y comprado dos veces.
	ponerPrecio(t, st, "mercadona", "m1", 1.8, 0, "")
	// Al peso: la tienda solo publica €/kg.
	ponerPrecio(t, st, "mercadona", "m3", 0, 1.15, "l")
	// Con precio manual por encima del de la tienda.
	ponerPrecio(t, st, "ahorramas", "a1", 3.5, 0, "")
	if err := st.SetPrecioManual(PrecioManual{Chain: "ahorramas", ProductURL: "a1", Precio: 2.0}); err != nil {
		t.Fatalf("SetPrecioManual: %v", err)
	}

	if _, err := st.AddToList("mercadona", "m1", 2); err != nil {
		t.Fatalf("AddToList: %v", err)
	}
	if _, err := st.AddToList("mercadona", "m3", 1); err != nil {
		t.Fatalf("AddToList: %v", err)
	}
	primera, err := st.AddToList("ahorramas", "a1", 1)
	if err != nil {
		t.Fatalf("AddToList: %v", err)
	}
	// Lo que devuelve AddToList ya viene resuelto.
	if primera.Precio != 2.0 || primera.Fuente != "manual" || primera.Subtotal != 2.0 {
		t.Fatalf("línea devuelta = %+v", primera)
	}

	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if len(lista.Items) != 3 {
		t.Fatalf("lista = %+v", lista)
	}
	// El orden es el en que se añadieron: la posición va siendo el máximo más uno.
	if lista.Items[0].ProductURL != "m1" || lista.Items[1].ProductURL != "m3" || lista.Items[2].ProductURL != "a1" {
		t.Fatalf("orden = %q %q %q", lista.Items[0].ProductURL, lista.Items[1].ProductURL, lista.Items[2].ProductURL)
	}

	web := lista.Items[0]
	if web.Precio != 1.8 || web.Fuente != "web" || web.Subtotal != 3.6 {
		t.Fatalf("línea con precio web = %+v", web)
	}
	if web.Nombre != "Fresas naturales" || web.NombreCadena != "Mercadona" || web.Chain != "mercadona" {
		t.Fatalf("línea con precio web = %+v", web)
	}
	if web.PrecioSoloMedida || web.Añadido.IsZero() {
		t.Fatalf("línea con precio web = %+v", web)
	}

	// Lo vendido al peso no se puede sumar: su subtotal es 0 y cuenta aparte.
	peso := lista.Items[1]
	if peso.Precio != 0 || peso.Subtotal != 0 || peso.PrecioMedida != 1.15 || peso.Medida != "l" {
		t.Fatalf("línea al peso = %+v", peso)
	}
	if !peso.PrecioSoloMedida {
		t.Fatalf("la línea al peso no avisa de que no se puede sumar: %+v", peso)
	}

	// El precio manual manda sobre el de la tienda para el subtotal.
	manual := lista.Items[2]
	if manual.Precio != 2.0 || manual.Fuente != "manual" || manual.Subtotal != 2.0 {
		t.Fatalf("línea con precio manual = %+v", manual)
	}

	if lista.Total != 5.6 {
		t.Fatalf("total = %v", lista.Total)
	}
	if lista.SinPrecio != 1 {
		t.Fatalf("SinPrecio = %d", lista.SinPrecio)
	}
	if lista.SubtotalPorCadena["mercadona"] != 3.6 || lista.SubtotalPorCadena["ahorramas"] != 2.0 {
		t.Fatalf("subtotales = %+v", lista.SubtotalPorCadena)
	}
	// El subtotal por tienda solo recoge lo que se puede sumar, así que una
	// cadena con líneas al peso y líneas con precio aparece con la parte
	// sumable y sin el resto.
	if sub := lista.SubtotalPorCadena["mercadona"]; sub != 3.6 {
		t.Fatalf("subtotales = %+v", lista.SubtotalPorCadena)
	}
	if lista.SubtotalPorCadenaNombres["mercadona"] != "Mercadona" ||
		lista.SubtotalPorCadenaNombres["ahorramas"] != "Ahorramas" {
		t.Fatalf("nombres = %+v", lista.SubtotalPorCadenaNombres)
	}
}

func TestListaCompraVacia(t *testing.T) {
	st := abrir(t)
	preparar(t, st)

	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if len(lista.Items) != 0 || lista.Total != 0 || lista.SinPrecio != 0 {
		t.Fatalf("lista vacía = %+v", lista)
	}
	// Los mapas se devuelven vacíos y no nil: quien los serialice quiere {},
	// no null.
	if lista.SubtotalPorCadena == nil || lista.SubtotalPorCadenaNombres == nil {
		t.Fatalf("mapas nil = %+v", lista)
	}
}

// Un producto que no tiene precio de unidad no se puede sumar, diga lo que diga
// su precio por medida: inventar la bolsa no vale.
func TestListaLoVendidoAlPesoNoSeSuma(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	// Solo hay precio por medida, y además puesto a mano.
	ponerPrecio(t, st, "mercadona", "m1", 0, 1.65, "kg")
	if err := st.SetPrecioManual(PrecioManual{
		Chain: "mercadona", ProductURL: "m1", PrecioMedida: 2.5, Medida: "kg",
	}); err != nil {
		t.Fatalf("SetPrecioManual: %v", err)
	}

	if _, err := st.AddToList("mercadona", "m1", 1.5); err != nil {
		t.Fatalf("AddToList: %v", err)
	}
	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	it := lista.Items[0]
	if it.Subtotal != 0 || it.Precio != 0 || it.PrecioMedida != 2.5 || it.Fuente != "manual" {
		t.Fatalf("línea al peso = %+v", it)
	}
	if lista.Total != 0 || lista.SinPrecio != 1 {
		t.Fatalf("total = %v, SinPrecio = %d", lista.Total, lista.SinPrecio)
	}
	if len(lista.SubtotalPorCadena) != 0 {
		t.Fatalf("una línea que no se suma no puede tener subtotal: %+v", lista.SubtotalPorCadena)
	}
}

func TestSetListQuantity(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)
	ponerPrecio(t, st, "mercadona", "m1", 1.8, 0, "")
	if _, err := st.AddToList("mercadona", "m1", 1); err != nil {
		t.Fatalf("AddToList: %v", err)
	}

	if err := st.SetListQuantity("mercadona", "m1", 4); err != nil {
		t.Fatalf("SetListQuantity: %v", err)
	}
	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if lista.Items[0].Cantidad != 4 || lista.Items[0].Subtotal != 7.2 || lista.Total != 7.2 {
		t.Fatalf("cantidad = %+v", lista)
	}

	if err := st.SetListQuantity("mercadona", "m2", 2); err == nil {
		t.Fatal("se puede cambiar la cantidad de algo que no está en la lista")
	}
	if err := st.SetListQuantity("mercadona", "m1", 0); err == nil {
		t.Fatal("una cantidad de cero no vale: para eso está quitarlo de la lista")
	}
}

func TestAddToListErrores(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	for _, cantidad := range []float64{0, -1} {
		if _, err := st.AddToList("mercadona", "m1", cantidad); err == nil {
			t.Fatalf("AddToList(%v) = sin error", cantidad)
		}
	}
	_, err := st.AddToList("mercadona", "no-existe", 1)
	if err == nil || !strings.Contains(err.Error(), "no está en el catálogo") {
		t.Fatalf("un producto que no está = %v", err)
	}

	if _, err := st.AddToList("mercadona", "m1", 1); err != nil {
		t.Fatalf("AddToList: %v", err)
	}
	// Añadirlo otra vez no decide por su cuenta cuánto sumar.
	if _, err := st.AddToList("mercadona", "m1", 3); err == nil {
		t.Fatal("añadir dos veces el mismo producto no puede ser un silencio")
	}
	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if len(lista.Items) != 1 || lista.Items[0].Cantidad != 1 {
		t.Fatalf("lista = %+v", lista)
	}
}

func TestListaRemoveYClear(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)
	ponerPrecio(t, st, "mercadona", "m1", 1.8, 0, "")
	for _, url := range []string{"m1", "m2", "m3"} {
		if _, err := st.AddToList("mercadona", url, 1); err != nil {
			t.Fatalf("AddToList(%s): %v", url, err)
		}
	}

	if err := st.RemoveFromList("mercadona", "m2"); err != nil {
		t.Fatalf("RemoveFromList: %v", err)
	}
	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if len(lista.Items) != 2 || lista.Items[1].ProductURL != "m3" {
		t.Fatalf("lista = %+v", lista.Items)
	}
	// Quitar lo que no está no es un error.
	if err := st.RemoveFromList("mercadona", "m2"); err != nil {
		t.Fatalf("RemoveFromList repetido: %v", err)
	}

	if err := st.ClearList(); err != nil {
		t.Fatalf("ClearList: %v", err)
	}
	lista, err = st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if len(lista.Items) != 0 || lista.Total != 0 {
		t.Fatalf("la lista no se vació: %+v", lista)
	}
	// Vaciar una lista vacía tampoco falla.
	if err := st.ClearList(); err != nil {
		t.Fatalf("ClearList repetido: %v", err)
	}
}

// Un producto que se borra del catálogo se lleva su línea y su precio manual con
// él: si no, la lista hablaría de cosas que ya no existen.
func TestListaDesapareceConElProducto(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	ponerPrecio(t, st, "mercadona", "m1", 1.8, 0, "")
	if err := st.SetPrecioManual(PrecioManual{Chain: "mercadona", ProductURL: "m1", Precio: 1.5}); err != nil {
		t.Fatalf("SetPrecioManual: %v", err)
	}
	if _, err := st.AddToList("mercadona", "m1", 1); err != nil {
		t.Fatalf("AddToList: %v", err)
	}
	if _, err := st.AddToList("ahorramas", "a1", 1); err != nil {
		t.Fatalf("AddToList: %v", err)
	}

	p, _, err := st.Product("mercadona", "m1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if _, err := st.db.Exec(`DELETE FROM products WHERE id = ?`, p.ID); err != nil {
		t.Fatalf("borrar producto: %v", err)
	}

	lista, err := st.ListaCompra()
	if err != nil {
		t.Fatalf("ListaCompra: %v", err)
	}
	if len(lista.Items) != 1 || lista.Items[0].ProductURL != "a1" {
		t.Fatalf("la línea del producto borrado sigue ahí: %+v", lista.Items)
	}
	if n := contarFilas(t, st, "precios_manuales"); n != 0 {
		t.Fatalf("precios_manuales = %d filas tras borrar el producto", n)
	}
	if lista.Total != 0 {
		t.Fatalf("total = %v, quiero 0", lista.Total)
	}
}
