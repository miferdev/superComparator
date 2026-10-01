package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/store"
)

// cadenaFalsa es un adaptador de mentira: devuelve el sitemap que le digamos.
type cadenaFalsa struct {
	id       string
	entries  []chain.SitemapEntry
	fallaCon error
}

func (c *cadenaFalsa) ID() string { return c.id }

func (c *cadenaFalsa) Sitemap(context.Context) ([]chain.SitemapEntry, error) {
	if c.fallaCon != nil {
		return nil, c.fallaCon
	}
	return c.entries, nil
}

func (c *cadenaFalsa) Fetch(context.Context, string) (chain.Product, error) {
	return chain.Product{}, errors.New("no implementado")
}

func nuevaBase(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, ch := range []store.Chain{
		{ID: "mercadona", Nombre: "Mercadona", SitemapURL: "x", PreciosActivos: true, PrecioMaxHoras: 24},
		{ID: "dia", Nombre: "DÍA", SitemapURL: "x", PreciosActivos: true, PrecioMaxHoras: 24},
	} {
		if err := st.SeedChains([]store.Chain{ch}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestIndexChainGuardaNombreMedidaYEnlace(t *testing.T) {
	st := nuevaBase(t)
	ch := &cadenaFalsa{id: "mercadona", entries: []chain.SitemapEntry{
		{URL: "https://tienda.mercadona.es/product/1/leche-entera-hacendado-brick-1-l", Name: "leche entera hacendado brick 1 l", SKU: "1"},
		{URL: "https://tienda.mercadona.es/product/2/pipas-calabaza-peladas-150-g", Name: "pipas calabaza peladas 150 g", SKU: "2"},
	}}

	ix := NewIndexer([]chain.Chain{ch}, st, nil)
	if err := ix.IndexChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}

	p, ok, err := st.Product("mercadona", ch.entries[1].URL)
	if err != nil || !ok {
		t.Fatalf("producto no guardado: %v", err)
	}
	if p.Name != "pipas calabaza peladas 150 g" {
		t.Errorf("nombre = %q", p.Name)
	}
	if p.Format != "150 g" {
		t.Errorf("formato = %q, want 150 g", p.Format)
	}
	if p.MeasureUnit != "kg" || p.MeasureValue != 0.15 {
		t.Errorf("medida = %v %q, want 0.15 kg", p.MeasureValue, p.MeasureUnit)
	}
	if p.Price != 0 {
		t.Errorf("el indexador no debe poner precio, tiene %v", p.Price)
	}
	// Y debe quedar en la cola de precios.
	q, err := st.NextQueued(time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 2 {
		t.Fatalf("en cola = %d, want 2", len(q))
	}
}

// TestIndexChainEsIdempotente: reindexar no duplica ni pisa el precio.
func TestIndexChainEsIdempotente(t *testing.T) {
	st := nuevaBase(t)
	ch := &cadenaFalsa{id: "mercadona", entries: []chain.SitemapEntry{
		{URL: "https://tienda.mercadona.es/product/1/leche-entera-1-l", Name: "leche entera 1 l", SKU: "1"},
	}}
	ix := NewIndexer([]chain.Chain{ch}, st, nil)
	if err := ix.IndexChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	p, _, _ := st.Product("mercadona", ch.entries[0].URL)
	if err := st.SetPrice(p.ID, 1.09, "unidad", 1.09, "l", true); err != nil {
		t.Fatal(err)
	}

	ch.entries[0].Name = "leche entera brick 1 l"
	if err := ix.IndexChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if n := totalDe(t, st, "mercadona"); n != 1 {
		t.Errorf("total = %d, want 1 (no debe duplicar)", n)
	}
	p, _, _ = st.Product("mercadona", ch.entries[0].URL)
	if p.Price != 1.09 {
		t.Errorf("el precio se ha perdido al reindexar: %v", p.Price)
	}
	if p.Name != "leche entera brick 1 l" {
		t.Errorf("el nombre no se ha actualizado: %q", p.Name)
	}
}

// TestIndexChainMarcaLoDeDiaParaVisita: DÍA solo da la categoría en el sitemap,
// así que sus productos quedan marcados para que la cola los visite.
func TestIndexChainMarcaLoDeDiaParaVisita(t *testing.T) {
	st := nuevaBase(t)
	ch := &cadenaFalsa{id: "dia", entries: []chain.SitemapEntry{
		{URL: "https://www.dia.es/huevos-leche-y-mantequilla/leche/p/16065",
			Name: "leche", Category: "Huevos leche y mantequilla / Leche", SKU: "16065"},
	}}
	ix := NewIndexer([]chain.Chain{ch}, st, nil)
	if err := ix.IndexChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	p, _, _ := st.Product("dia", ch.entries[0].URL)
	if p.CrawlState != "ficha_pendiente" {
		t.Errorf("crawl_state = %q, want ficha_pendiente", p.CrawlState)
	}
	if p.NameSource != "categoria" {
		t.Errorf("name_source = %q, want categoria", p.NameSource)
	}
	if p.Category == "" {
		t.Error("debería guardar la categoría de la URL")
	}
}

// TestIndexChainConErrorNoRompe: una cadena que falla se anota y las demás siguen.
func TestIndexChainConErrorNoRompe(t *testing.T) {
	st := nuevaBase(t)
	rota := &cadenaFalsa{id: "mercadona", fallaCon: errors.New("boom")}
	buena := &cadenaFalsa{id: "dia", entries: []chain.SitemapEntry{
		{URL: "https://www.dia.es/huevos-leche-y-mantequilla/leche/p/1", Name: "leche", Category: "Leche", SKU: "1"},
	}}
	ix := NewIndexer([]chain.Chain{rota, buena}, st, nil)

	err := ix.IndexCatalog(context.Background())
	if err != nil {
		t.Fatalf("IndexCatalog no debe fallar por una cadena rota: %v", err)
	}
	estado, err := st.CrawlState("mercadona")
	if err != nil {
		t.Fatal(err)
	}
	if estado.Phase != "error" || !estado.Paused {
		t.Errorf("estado de la cadena rota = %+v", estado)
	}
	if n := totalDe(t, st, "dia"); n != 1 {
		t.Errorf("la cadena buena no se indexó: %d productos", n)
	}
	if n := totalDe(t, st, "mercadona"); n != 0 {
		t.Errorf("la cadena rota no debería tener productos: %d", n)
	}
}

// TestIndexChainRespetaElMaximo sirve para probar el indexador sin comerse los
// 100.000 productos de Alcampo.
func TestIndexChainRespetaElMaximo(t *testing.T) {
	st := nuevaBase(t)
	var entries []chain.SitemapEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, chain.SitemapEntry{
			URL:  "https://tienda.mercadona.es/product/" + itoa(i) + "/producto-" + itoa(i),
			Name: "producto " + itoa(i) + " 500 g",
		})
	}
	ch := &cadenaFalsa{id: "mercadona", entries: entries}
	ix := NewIndexer([]chain.Chain{ch}, st, nil).WithOptions(IndexerOptions{MaxProducts: 10})
	if err := ix.IndexChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if n := totalDe(t, st, "mercadona"); n != 10 {
		t.Errorf("total = %d, want 10", n)
	}
}

// totalDe devuelve cuántos productos hay guardados de una cadena.
func totalDe(t *testing.T, st *store.Store, chainID string) int {
	t.Helper()
	counts, err := st.CatalogCounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range counts {
		if c.Chain == chainID {
			return c.Total
		}
	}
	return 0
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// TestIndexChainNoEncolaSiNoHayPrecios: Alcampo tiene 89.615 productos y su WAF
// bloquea las fichas, así que encolarlos sería trabajo que nunca puede salir.
func TestIndexChainNoEncolaSiNoHayPrecios(t *testing.T) {
	st := nuevaBase(t)
	if _, err := st.DB().Exec(`UPDATE chains SET precios_activos = 0 WHERE id = 'mercadona'`); err != nil {
		t.Fatal(err)
	}
	ch := &cadenaFalsa{id: "mercadona", entries: []chain.SitemapEntry{
		{URL: "https://tienda.mercadona.es/product/1/leche-1-l", Name: "leche 1 l", SKU: "1"},
	}}
	if err := NewIndexer([]chain.Chain{ch}, st, nil).IndexChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	q, err := st.NextQueued(time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 0 {
		t.Errorf("cola = %d, want 0 con los precios desactivados", len(q))
	}
	if n := totalDe(t, st, "mercadona"); n != 1 {
		t.Errorf("el catálogo debe guardarse igual: %d productos", n)
	}
}
