package store

import (
	"path/filepath"
	"testing"
	"time"
)

func abrir(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "catalogo.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func cadenasCatalogo() []Chain {
	return []Chain{
		{ID: "mercadona", Nombre: "Mercadona", SitemapURL: "https://www.mercadona.es/sitemap.xml", PreciosActivos: true, PausaSegundos: 2, Concurrencia: 2, PrecioMaxHoras: 24},
		{ID: "ahorramas", Nombre: "Ahorramas", SitemapURL: "https://www.ahorramas.com/sitemap.xml", PreciosActivos: true, PausaSegundos: 3, Concurrencia: 1, PrecioMaxHoras: 12},
		{ID: "dia", Nombre: "DÍA", SitemapURL: "https://www.dia.es/sitemap.xml", PreciosActivos: true, PausaSegundos: 2, Concurrencia: 1, PrecioMaxHoras: 24},
		{ID: "alcampo", Nombre: "Alcampo", SitemapURL: "https://www.alcampo.es/sitemaps/0_sitemap_index.xml", PreciosActivos: false, PausaSegundos: 5, Concurrencia: 1, PrecioMaxHoras: 48},
	}
}

func preparar(t *testing.T, st *Store) {
	t.Helper()
	if err := st.SeedChains(cadenasCatalogo()); err != nil {
		t.Fatalf("SeedChains: %v", err)
	}
}

func TestSeedChainsIdempotente(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	if err := st.SeedChains(cadenasCatalogo()); err != nil {
		t.Fatalf("SeedChains repetido: %v", err)
	}

	chains, err := st.Chains()
	if err != nil {
		t.Fatalf("Chains: %v", err)
	}
	if len(chains) != 4 {
		t.Fatalf("chains = %d, %+v", len(chains), chains)
	}
	if chains[0].ID != "ahorramas" || chains[0].PausaSegundos != 3 || !chains[0].PreciosActivos {
		t.Fatalf("chains[0] = %+v", chains[0])
	}
	if chains[3].ID != "mercadona" {
		t.Fatalf("chains[3] = %+v", chains[3])
	}
	c, ok, err := st.Chain("dia")
	if err != nil || !ok || c.Nombre != "DÍA" {
		t.Fatalf("Chain(dia) = %+v, %v, %v", c, ok, err)
	}
	if _, ok, err := st.Chain("carrefour"); ok || err != nil {
		t.Fatalf("Chain(carrefour) debería no existir: %v %v", ok, err)
	}
}

// Un arranque no debe deshacer lo que el usuario ajustó a mano.
func TestSeedChainsNoPisaAjustes(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	if _, err := st.db.Exec(`UPDATE chains SET precios_activos = 0, pausa_segundos = 30, nombre = '' WHERE id = 'dia'`); err != nil {
		t.Fatalf("ajuste: %v", err)
	}
	if err := st.SeedChains(cadenasCatalogo()); err != nil {
		t.Fatalf("SeedChains: %v", err)
	}
	c, _, err := st.Chain("dia")
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if c.PreciosActivos || c.PausaSegundos != 30 {
		t.Fatalf("seed pisó los ajustes: %+v", c)
	}
	if c.Nombre != "DÍA" {
		t.Fatalf("nombre vacío sin rellenar: %+v", c)
	}
}

func TestUpsertProductsNoDuplicaNiPisaPrecio(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	lote := []Product{
		{URL: "https://mercadona.es/p/1", SKU: "1", Name: "Fresas naturales", SearchName: "fresas naturales", Category: "fruta"},
		{URL: "https://mercadona.es/p/2", Name: "Leche entera 1 L", Format: "brick", MeasureValue: 1, MeasureUnit: "l"},
	}
	inserted, updated, err := st.UpsertProducts("mercadona", lote)
	if err != nil || inserted != 2 || updated != 0 {
		t.Fatalf("primera pasada = %d insertados, %d actualizados, %v", inserted, updated, err)
	}
	if err := st.SetPrice(1, 3.75, "kg", 8.25, "kg", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}

	lote[0].Name = "Fresas naturales Extra"
	lote[0].SearchName = "fresas naturales extra"
	lote[0].Category = "fruta"
	inserted, updated, err = st.UpsertProducts("mercadona", lote)
	if err != nil || inserted != 0 || updated != 2 {
		t.Fatalf("segunda pasada = %d insertados, %d actualizados, %v", inserted, updated, err)
	}

	p, ok, err := st.Product("mercadona", "https://mercadona.es/p/1")
	if err != nil || !ok {
		t.Fatalf("Product: %v %v", ok, err)
	}
	if p.Name != "Fresas naturales Extra" {
		t.Fatalf("no actualizó el nombre: %+v", p)
	}
	if p.Price != 3.75 || p.PriceFetchedAt.IsZero() {
		t.Fatalf("el reindexado pisó el precio: %+v", p)
	}
	if p.CrawlState != "catalogado" || !p.Available {
		t.Fatalf("estado por defecto inesperado: %+v", p)
	}
	segunda, ok, err := st.ProductByID(p.ID)
	if err != nil || !ok || segunda.URL != p.URL {
		t.Fatalf("ProductByID = %+v %v %v", segunda, ok, err)
	}
	if !segunda.FirstSeen.Equal(p.FirstSeen) || !segunda.LastSeen.Equal(p.LastSeen) {
		t.Fatalf("first_seen cambió: %+v %+v", p.FirstSeen, segunda.FirstSeen)
	}

	// Un lote vacío no toca nada.
	inserted, updated, err = st.UpsertProducts("mercadona", nil)
	if err != nil || inserted != 0 || updated != 0 {
		t.Fatalf("lote vacío = %d, %d, %v", inserted, updated, err)
	}
}

func TestCatalogCounts(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	if _, _, err := st.UpsertProducts("mercadona", []Product{
		{URL: "u1", Name: "Fresas"},
		{URL: "u2", Name: "Leche"},
		{URL: "u3", Name: "Pan"},
	}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	if _, _, err := st.UpsertProducts("ahorramas", []Product{{URL: "u4", Name: "Yogur"}}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	p, _, err := st.Product("mercadona", "u1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if err := st.SetPrice(p.ID, 2.5, "kg", 2.5, "kg", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}

	counts, err := st.CatalogCounts()
	if err != nil {
		t.Fatalf("CatalogCounts: %v", err)
	}
	if len(counts) != 4 {
		t.Fatalf("counts = %+v", counts)
	}
	porCadena := map[string]CatalogCount{}
	for _, c := range counts {
		porCadena[c.Chain] = c
	}
	if c := porCadena["mercadona"]; c.Total != 3 || c.ConPrecio != 1 || c.SinPrecio != 2 {
		t.Fatalf("mercadona = %+v", c)
	}
	if c := porCadena["ahorramas"]; c.Total != 1 || c.ConPrecio != 0 || c.SinPrecio != 1 {
		t.Fatalf("ahorramas = %+v", c)
	}
	if c := porCadena["dia"]; c.Total != 0 || c.ConPrecio != 0 || c.SinPrecio != 0 {
		t.Fatalf("dia = %+v", c)
	}
}

func sembrar(t *testing.T, st *Store) {
	t.Helper()
	if _, _, err := st.UpsertProducts("mercadona", []Product{
		{URL: "m1", Name: "Fresas naturales", SearchName: "fresas naturales", Category: "fruta"},
		{URL: "m2", Name: "Fresas del bosque", SearchName: "fresas del bosque", Category: "fruta"},
		{URL: "m3", Name: "Leche entera", SearchName: "leche entera", Category: "lacteos"},
	}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	if _, _, err := st.UpsertProducts("ahorramas", []Product{
		{URL: "a1", Name: "Mermelada de fresa", SearchName: "mermelada de fresa"},
	}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
}

func TestSearchFresas(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	for texto, quiero := range map[string]int{
		"fresa": 3, "FRESA": 3, "fresas": 2, "Fresas": 2,
	} {
		res, err := st.Search(SearchQuery{Text: texto})
		if err != nil {
			t.Fatalf("Search(%q): %v", texto, err)
		}
		if res.Total != quiero {
			t.Fatalf("Search(%q).Total = %d, quiero %d: %+v", texto, res.Total, quiero, res.Products)
		}
	}
	res, err := st.Search(SearchQuery{Text: "fresa"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Products) != 3 || res.HayMas {
		t.Fatalf("página = %d productos, hayMas=%v", len(res.Products), res.HayMas)
	}

	// La coma del plural no es una palabra: "fresas naturales" tiene que
	// encontrar la ficha entera.
	res, err = st.Search(SearchQuery{Text: "fresas naturales"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Products[0].URL != "m1" {
		t.Fatalf("dos términos = %+v", res)
	}
}

func TestSearchFiltrosYOrden(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)
	precios := map[string]float64{"m1": 4.0, "m2": 2.0, "a1": 3.0}
	for url, precio := range precios {
		chain := "mercadona"
		if url == "a1" {
			chain = "ahorramas"
		}
		p, _, err := st.Product(chain, url)
		if err != nil {
			t.Fatalf("Product: %v", err)
		}
		if err := st.SetPrice(p.ID, precio, "kg", precio*2, "kg", true); err != nil {
			t.Fatalf("SetPrice: %v", err)
		}
	}

	res, err := st.Search(SearchQuery{Text: "fresa", Chain: "mercadona"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("filtro de cadena = %+v", res)
	}

	res, err = st.Search(SearchQuery{Text: "fresa", ConPrecio: true, Orden: "precio_asc"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("ConPrecio = %+v", res)
	}
	if res.Products[0].Price != 2.0 || res.Products[1].Price != 3.0 || res.Products[2].Price != 4.0 {
		t.Fatalf("orden por precio = %v", res)
	}

	res, err = st.Search(SearchQuery{Text: "fresa", ConPrecio: true, Orden: "precio_desc"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Products[0].Price != 4.0 {
		t.Fatalf("precio_desc = %v", res)
	}

	res, err = st.Search(SearchQuery{Text: "fresa", Orden: "nombre"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Products[0].Name != "Fresas del bosque" {
		t.Fatalf("orden por nombre = %v", res.Products[0])
	}

	// Sin texto se devuelve el catálogo entero.
	res, err = st.Search(SearchQuery{Orden: "precio_asc"})
	if err != nil {
		t.Fatalf("Search sin texto: %v", err)
	}
	if res.Total != 4 {
		t.Fatalf("catálogo = %+v", res)
	}

	res, err = st.Search(SearchQuery{Limit: 2})
	if err != nil {
		t.Fatalf("Search paginado: %v", err)
	}
	if len(res.Products) != 2 || res.Total != 4 || !res.HayMas {
		t.Fatalf("página = %d total=%d hayMas=%v", len(res.Products), res.Total, res.HayMas)
	}
	res, err = st.Search(SearchQuery{Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("Search segunda página: %v", err)
	}
	if len(res.Products) != 2 || res.HayMas {
		t.Fatalf("segunda página = %d hayMas=%v", len(res.Products), res.HayMas)
	}
}

// La búsqueda tiene que aguantar acentos y ñ, que es como se escriben en las
// fichas pero no siempre en el teclado.
func TestSearchAcentos(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	if _, _, err := st.UpsertProducts("mercadona", []Product{
		{URL: "m1", Name: "Jamón de jamón", SearchName: "jamón de jamón"},
	}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	for _, texto := range []string{"jamon", "jamón", "Jamón"} {
		res, err := st.Search(SearchQuery{Text: texto})
		if err != nil {
			t.Fatalf("Search(%q): %v", texto, err)
		}
		if res.Total != 1 {
			t.Fatalf("Search(%q) = %+v", texto, res)
		}
	}
}

func TestFTSSigueAlProducto(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	res, err := st.Search(SearchQuery{Text: "fresa", Chain: "ahorramas"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Products[0].Name != "Mermelada de fresa" {
		t.Fatalf("antes de renombrar = %+v", res)
	}

	// Al reindexar con otro nombre, el índice tiene que seguir al producto.
	p, _, err := st.Product("ahorramas", "a1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if _, _, err := st.UpsertProducts("ahorramas", []Product{
		{URL: "a1", Name: "Mermelada de arándanos", SearchName: "mermelada de arándanos"},
	}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	res, err = st.Search(SearchQuery{Text: "fresa"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("el índice tiene el nombre viejo: %+v", res)
	}
	res, err = st.Search(SearchQuery{Text: "arándanos"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Products[0].ID != p.ID {
		t.Fatalf("el índice no tiene el nombre nuevo: %+v", res)
	}

	if _, err := st.db.Exec(`DELETE FROM products WHERE id = ?`, p.ID); err != nil {
		t.Fatalf("borrar: %v", err)
	}
	res, err = st.Search(SearchQuery{Text: "arándanos"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("el índice no se limpió al borrar: %+v", res)
	}
}

func TestColaDePrecios(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)
	if _, _, err := st.UpsertProducts("ahorramas", []Product{{URL: "a2", Name: "Pan de molde"}}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}

	n, err := st.EnqueuePrices("mercadona", 0)
	if err != nil || n != 3 {
		t.Fatalf("EnqueuePrices = %d, %v", n, err)
	}
	if n, err = st.EnqueuePrices("mercadona", 0); err != nil || n != 0 {
		t.Fatalf("ya estaban en la cola: %d, %v", n, err)
	}
	if n, err = st.EnqueuePrices("ahorramas", 1); err != nil || n != 1 {
		t.Fatalf("EnqueuePrices limitado = %d, %v", n, err)
	}

	cola, err := st.NextQueued(time.Now(), 10)
	if err != nil {
		t.Fatalf("NextQueued: %v", err)
	}
	if len(cola) != 4 {
		t.Fatalf("cola = %+v", cola)
	}
	for _, q := range cola {
		if q.Chain == "" || q.URL == "" {
			t.Fatalf("falta la ficha: %+v", q)
		}
	}

	// Marcadas como descargando, no vuelven a salir.
	if cola, err = st.NextQueued(time.Now(), 10); err != nil || len(cola) != 0 {
		t.Fatalf("cola repetida = %+v, %v", cola, err)
	}

	// Un fallo devuelve la entrada con el backoff puesto.
	proximo := time.Now().Add(5 * time.Minute)
	estado, err := st.FailQueue(colaID(t, st, "m1"), 1, proximo, "timeout")
	if err != nil || estado != "pendiente" {
		t.Fatalf("FailQueue = %q, %v", estado, err)
	}
	if cola, err = st.NextQueued(time.Now().Add(time.Hour), 10); err != nil || len(cola) != 1 {
		t.Fatalf("cola tras el fallo = %+v, %v", cola, err)
	}
	if cola[0].Attempts != 1 || cola[0].LastError != "timeout" {
		t.Fatalf("intentos = %+v", cola[0])
	}
	if cola, err = st.NextQueued(time.Now().Add(time.Minute), 10); err != nil || len(cola) != 0 {
		t.Fatalf("salió antes de su momento: %+v, %v", cola, err)
	}
	if _, err := st.FailQueue(colaID(t, st, "m1"), 2, proximo, "timeout otra vez"); err != nil {
		t.Fatalf("FailQueue: %v", err)
	}
	cola, err = st.NextQueued(time.Now().Add(time.Hour), 10)
	if err != nil || len(cola) != 1 || cola[0].Attempts != 2 {
		t.Fatalf("los intentos no se acumulan: %+v, %v", cola, err)
	}

	if err := st.MarkQueueDone(cola[0].ProductID); err != nil {
		t.Fatalf("MarkQueueDone: %v", err)
	}
	if cola, err = st.NextQueued(time.Now().Add(time.Hour), 10); err != nil || len(cola) != 0 {
		t.Fatalf("la cola no se vació: %+v, %v", cola, err)
	}
	// Con precio ya no tiene sentido encolar el producto.
	p, _, err := st.Product("ahorramas", "a1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if err := st.SetPrice(p.ID, 1.99, "kg", 1.99, "kg", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	if err := st.MarkQueueDone(p.ID); err != nil {
		t.Fatalf("MarkQueueDone: %v", err)
	}
	if n, err = st.EnqueuePrices("ahorramas", 0); err != nil || n != 1 {
		t.Fatalf("encolados tras poner precio = %d, %v", n, err)
	}
	var enCola int
	if err := st.db.QueryRow(`SELECT count(*) FROM price_queue WHERE product_id = ?`, p.ID).
		Scan(&enCola); err != nil {
		t.Fatalf("cola: %v", err)
	}
	if enCola != 0 {
		t.Fatalf("el producto con precio sigue en la cola")
	}
}

func colaID(t *testing.T, st *Store, url string) int64 {
	t.Helper()
	p, ok, err := st.Product("mercadona", url)
	if err != nil || !ok {
		t.Fatalf("Product(%s): %v %v", url, ok, err)
	}
	return p.ID
}

func TestSetPriceGuardaHistorial(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)
	p, _, err := st.Product("mercadona", "m3")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if err := st.SetPrice(p.ID, 1.15, "l", 1.15, "l", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}

	got, _, err := st.Product("mercadona", "m3")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if got.Price != 1.15 || got.PriceBasis != "l" || got.MeasurePrice != 1.15 {
		t.Fatalf("producto = %+v", got)
	}
	if got.PriceFetchedAt.IsZero() || !got.Available {
		t.Fatalf("producto = %+v", got)
	}

	latest, ok, err := st.LatestPrice("mercadona", "m3")
	if err != nil || !ok {
		t.Fatalf("LatestPrice = %v %v", ok, err)
	}
	if latest.Price != 1.15 || latest.Name != "Leche entera" || latest.MeasureUnit != "l" {
		t.Fatalf("historial = %+v", latest)
	}
	var productID int64
	if err := st.db.QueryRow(`SELECT product_id FROM price_history WHERE product_url = 'm3'`).
		Scan(&productID); err != nil {
		t.Fatalf("product_id del historial: %v", err)
	}
	if productID != p.ID {
		t.Fatalf("product_id = %d, quiero %d", productID, p.ID)
	}

	if err := st.SetPrice(9999, 1, "", 1, "", true); err == nil {
		t.Fatal("SetPrice de un producto inexistente no falla")
	}
}

// Si FTS5 no está disponible o rechaza la consulta, buscar no puede fallar.
func TestSearchCaeALike(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)
	if _, err := st.db.Exec(`DROP TABLE products_fts`); err != nil {
		t.Fatalf("drop fts: %v", err)
	}
	res, err := st.Search(SearchQuery{Text: "fresas"})
	if err != nil {
		t.Fatalf("Search sin FTS: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("LIKE = %+v", res)
	}
}

func TestRellenaHistorialViejo(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	if _, _, err := st.UpsertProducts("mercadona", []Product{{URL: "m1", Name: "Fresas naturales"}}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	// Historial escrito por el esquema antiguo: sin product_id.
	if _, err := st.db.Exec(`
		INSERT INTO price_history (chain, product_url, name, price, fetched_at) VALUES
			('mercadona', 'm1', 'Fresas naturales', 2.5, '2026-01-01T10:00:00Z'),
			('mercadona', 'desaparecido', 'Pan de molde', 1.1, '2026-01-01T10:00:00Z')`); err != nil {
		t.Fatalf("historial antiguo: %v", err)
	}
	if err := backfillHistory(st.db); err != nil {
		t.Fatalf("backfillHistory: %v", err)
	}

	p, _, err := st.Product("mercadona", "m1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	var conID int
	if err := st.db.QueryRow(`SELECT count(*) FROM price_history WHERE product_url = 'm1' AND product_id = ?`, p.ID).
		Scan(&conID); err != nil {
		t.Fatalf("product_id: %v", err)
	}
	if conID != 1 {
		t.Fatalf("el historial antiguo no se emparejó (%d filas)", conID)
	}
	var huerfanas int
	if err := st.db.QueryRow(`SELECT count(*) FROM price_history WHERE product_url = 'desaparecido' AND product_id IS NULL`).
		Scan(&huerfanas); err != nil {
		t.Fatalf("historial huérfano: %v", err)
	}
	if huerfanas != 1 {
		t.Fatalf("una ficha que no está no debería emparejarse con nada")
	}
	var filas int
	if err := st.db.QueryRow(`SELECT count(*) FROM price_history`).Scan(&filas); err != nil {
		t.Fatalf("price_history: %v", err)
	}
	if filas != 2 {
		t.Fatalf("price_history perdió filas: %d", filas)
	}
}

func TestCrawlState(t *testing.T) {
	st := abrir(t)
	preparar(t, st)

	st0, err := st.CrawlState("mercadona")
	if err != nil {
		t.Fatalf("CrawlState: %v", err)
	}
	if st0.Chain != "mercadona" || st0.Done != 0 || !st0.UpdatedAt.IsZero() {
		t.Fatalf("estado inexistente = %+v", st0)
	}

	if err := st.SetCrawlState(CrawlState{
		Chain: "mercadona", Phase: "sitemaps", Cursor: "sitemap_3.xml", Done: 120, Total: 900,
	}); err != nil {
		t.Fatalf("SetCrawlState: %v", err)
	}
	got, err := st.CrawlState("mercadona")
	if err != nil {
		t.Fatalf("CrawlState: %v", err)
	}
	if got.Phase != "sitemaps" || got.Cursor != "sitemap_3.xml" || got.Done != 120 || got.Total != 900 {
		t.Fatalf("estado = %+v", got)
	}
	if got.Paused || got.UpdatedAt.IsZero() {
		t.Fatalf("estado = %+v", got)
	}

	if err := st.SetCrawlState(CrawlState{
		Chain: "mercadona", Phase: "fichas", Cursor: "sitemap_4.xml", Done: 800, Total: 900, Paused: true,
	}); err != nil {
		t.Fatalf("SetCrawlState: %v", err)
	}
	got, err = st.CrawlState("mercadona")
	if err != nil {
		t.Fatalf("CrawlState: %v", err)
	}
	if got.Phase != "fichas" || got.Done != 800 || !got.Paused {
		t.Fatalf("estado actualizado = %+v", got)
	}
}

func TestRuns(t *testing.T) {
	st := abrir(t)
	if _, ok, err := st.LastRun(); ok || err != nil {
		t.Fatalf("LastRun sin ejecuciones: %v %v", ok, err)
	}
	id, err := st.StartRun("catalogo", "mercadona")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := st.FinishRun(id, 42, 2, "una cadena se quedó a medias"); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	r, ok, err := st.LastRun()
	if err != nil || !ok {
		t.Fatalf("LastRun = %v %v", ok, err)
	}
	if r.ID != id || r.Kind != "catalogo" || r.Chain != "mercadona" || r.Products != 42 || r.Errors != 2 {
		t.Fatalf("run = %+v", r)
	}
	if r.StartedAt.IsZero() || r.FinishedAt.IsZero() {
		t.Fatalf("run = %+v", r)
	}
}

// Reabrir la base no debe volver a aplicar migraciones ni perder el catálogo.
func TestMigracionesSoloUnaVez(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalogo.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	preparar(t, st)
	if _, _, err := st.UpsertProducts("mercadona", []Product{{URL: "m1", Name: "Fresas"}}); err != nil {
		t.Fatalf("UpsertProducts: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer st.Close()
	var aplicadas int
	if err := st.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&aplicadas); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if aplicadas != len(migrations) {
		t.Fatalf("migraciones = %d, quiero %d", aplicadas, len(migrations))
	}
	res, err := st.Search(SearchQuery{Text: "fresas"})
	if err != nil || res.Total != 1 {
		t.Fatalf("el catálogo se perdió al reabrir: %+v, %v", res, err)
	}
}

func TestFichaData(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	// El caso de DÍÁ: el sitemap solo traía la categoría, así que el producto
	// está sin nombre real y marcado como pendiente de ficha.
	if _, err := st.db.Exec(`
		INSERT INTO products (chain, url, name, search_name, format, category, name_source,
			crawl_state, available, first_seen, last_seen)
		VALUES ('dia', '/p/1', 'Leches', 'leches', '1 L', 'Leches', 'categoria',
			'ficha_pendiente', 1, ?, ?)`, ts(time.Now()), ts(time.Now())); err != nil {
		t.Fatalf("sembrar dia: %v", err)
	}

	var id int64
	if err := st.db.QueryRow(`SELECT id FROM products WHERE chain = 'dia' AND url = '/p/1'`).Scan(&id); err != nil {
		t.Fatalf("id: %v", err)
	}

	if _, _, err := st.FichaData(id, "Leche entera Asturiana 1 L", "leche entera asturiana",
		"Lácteos / Leches"); err != nil {
		t.Fatalf("FichaData: %v", err)
	}

	var name, search, category, source, state string
	if err := st.db.QueryRow(`
		SELECT name, search_name, category, name_source, crawl_state
		FROM products WHERE id = ?`, id).
		Scan(&name, &search, &category, &source, &state); err != nil {
		t.Fatalf("leer: %v", err)
	}
	if name != "Leche entera Asturiana 1 L" || search != "leche entera asturiana" {
		t.Fatalf("nombre = %q, search_name = %q", name, search)
	}
	if category != "Lácteos / Leches" {
		t.Fatalf("categoría = %q", category)
	}
	// Con nombre de ficha el producto deja de estar pendiente.
	if source != "ficha" || state != "catalogado" {
		t.Fatalf("name_source = %q, crawl_state = %q", source, state)
	}

	// Y ahora tiene que aparecer por su nombre de verdad, que es el motivo.
	res, err := st.Search(SearchQuery{Text: "asturiana"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 || res.Products[0].Name != "Leche entera Asturiana 1 L" {
		t.Fatalf("el nombre de la ficha no se busca: %+v", res)
	}
}

func TestFichaDataNoDegradaLoBueno(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrar(t, st)

	p, _, err := st.Product("ahorramas", "a1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}

	// Una ficha sin nombre no puede tirar abajo el nombre del sitemap, ni
	// convertir sus tokens en el nombre en crudo.
	if _, _, err := st.FichaData(p.ID, "", "", "Alimentación / Legumbres"); err != nil {
		t.Fatalf("FichaData: %v", err)
	}
	var name, search string
	if err := st.db.QueryRow(`SELECT name, search_name FROM products WHERE id = ?`, p.ID).
		Scan(&name, &search); err != nil {
		t.Fatalf("leer: %v", err)
	}
	if name != "Mermelada de fresa" {
		t.Fatalf("name = %q", name)
	}
	if search == "" || search == name {
		t.Fatalf("search_name se degradó a %q", search)
	}
}

func TestFichaDataProductoQueNoEsta(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	if _, _, err := st.FichaData(999, "Leche", "leche", ""); err == nil {
		t.Fatal("un producto que no está no puede escribirse en silencio")
	}
}
