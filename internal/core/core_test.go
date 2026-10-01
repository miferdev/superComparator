package core

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/store"
)

type fakeChain struct {
	entries  []chain.SitemapEntry
	products map[string]chain.Product
}

func (f *fakeChain) ID() string { return "mercadona" }

func (f *fakeChain) Sitemap(context.Context) ([]chain.SitemapEntry, error) { return f.entries, nil }

func (f *fakeChain) Fetch(_ context.Context, url string) (chain.Product, error) {
	p, ok := f.products[url]
	if !ok {
		return chain.Product{}, chain.ErrNotFound
	}
	return p, nil
}

type collector struct {
	mu     sync.Mutex
	events []Event
}

func (c *collector) add(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *collector) has(match func(Event) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if match(e) {
			return true
		}
	}
	return false
}

func newTestCore(t *testing.T) (*Core, *fakeChain) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	url := "https://tienda.mercadona.es/product/1/leche-entera-hacendado-brick-1-l"
	fake := &fakeChain{
		entries: []chain.SitemapEntry{
			{URL: url, Name: "leche entera hacendado brick 1 l"},
			{URL: "https://tienda.mercadona.es/product/2/pan-de-molde", Name: "pan de molde"},
		},
		products: map[string]chain.Product{
			url: {
				Chain: "mercadona", URL: url, SKU: "1",
				Name: "Leche entera Hacendado Brick 1 L", Price: 2.45,
				MeasurePrice: 2.45, MeasureUnit: "l", Available: true, FetchedAt: time.Now(),
			},
		},
	}
	cfg := config.Config{PostalCode: "28032", Workers: 2, Candidates: 3, Timeout: time.Second, Delay: 0}
	return New(cfg, []chain.Chain{fake}, st, slog.New(slog.NewTextHandler(io.Discard, nil))), fake
}

func TestResolveCheckCompare(t *testing.T) {
	ctx := context.Background()
	c, fake := newTestCore(t)
	col := &collector{}

	items := []list.Item{{Name: "leche entera 1l", Quantity: 3}}
	if err := c.Resolve(ctx, items, col.add); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !col.has(func(e Event) bool { _, ok := e.(ChainResolved); return ok }) {
		t.Fatalf("sin evento ChainResolved: %+v", col.events)
	}

	cmp, err := c.Comparison(ctx)
	if err != nil {
		t.Fatalf("Comparison: %v", err)
	}
	if diff := cmp.Totals["mercadona"] - 2.45*3; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("totales = %+v", cmp.Totals)
	}
	if cmp.CheapestChain != "mercadona" || len(cmp.Items) != 1 || cmp.Items[0].Cheapest != "mercadona" {
		t.Fatalf("comparativa = %+v", cmp)
	}

	updated := fake.products[fake.entries[0].URL]
	updated.Price = 2.60
	updated.FetchedAt = time.Now().Add(time.Second)
	fake.products[fake.entries[0].URL] = updated

	col = &collector{}
	if err := c.Check(ctx, col.add); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !col.has(func(e Event) bool {
		pc, ok := e.(PriceChanged)
		return ok && pc.Old == 2.45 && pc.New == 2.60
	}) {
		t.Fatalf("sin evento PriceChanged: %+v", col.events)
	}
}

func TestCheapestAcceptableOption(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	expensive := "https://tienda.mercadona.es/product/1/leche-entera"
	cheap := "https://tienda.mercadona.es/product/2/leche-entera-hacendado-brick"
	fake := &fakeChain{
		entries: []chain.SitemapEntry{
			{URL: expensive, Name: "leche entera brick 1 l"},
			{URL: cheap, Name: "leche entera hacendado brick 1 l"},
		},
		products: map[string]chain.Product{
			expensive: {
				Chain: "mercadona", URL: expensive, SKU: "1", Name: "Leche entera",
				Format: "Brik 1 L", Price: 5, MeasurePrice: 5, MeasureUnit: "l",
				Available: true, FetchedAt: time.Now(),
			},
			cheap: {
				Chain: "mercadona", URL: cheap, SKU: "2", Name: "Leche entera Hacendado",
				Format: "Brik 1 L", Price: 0.96, MeasurePrice: 0.96, MeasureUnit: "l",
				Available: true, FetchedAt: time.Now(),
			},
		},
	}
	cfg := config.Config{Workers: 1, Candidates: 3, Timeout: time.Second}
	c := New(cfg, []chain.Chain{fake}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))

	col := &collector{}
	if err := c.Resolve(context.Background(), []list.Item{{Name: "leche entera 1l", Quantity: 1}}, col.add); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	matches, err := st.Matches()
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %+v, %v", matches, err)
	}
	if matches[0].URL != cheap {
		t.Fatalf("se esperaba la opción más barata (%s), se eligió %s", cheap, matches[0].URL)
	}
	if !col.has(func(e Event) bool {
		cr, ok := e.(ChainResolved)
		if !ok || len(cr.Alternatives) == 0 {
			return false
		}
		for _, a := range cr.Alternatives {
			if a.Price <= 0 || a.MeasurePrice <= 0 || a.MeasureUnit != "l" {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("las alternativas deben llevar precio y €/medida: %+v", col.events)
	}
}

func TestDelisted(t *testing.T) {
	ctx := context.Background()
	c, fake := newTestCore(t)
	col := &collector{}

	if err := c.Resolve(ctx, []list.Item{{Name: "leche entera 1l", Quantity: 1}}, col.add); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	delete(fake.products, fake.entries[0].URL)

	col = &collector{}
	if err := c.Check(ctx, col.add); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !col.has(func(e Event) bool { _, ok := e.(ProductDelisted); return ok }) {
		t.Fatalf("sin evento ProductDelisted: %+v", col.events)
	}
}

// cadenaConID es un fakeChain con identificador propio, para poder montar
// comparativas con varias cadenas.
type cadenaConID struct {
	*fakeChain
	id string
}

func (f cadenaConID) ID() string { return f.id }

// nuevoCoreConCadenas monta un núcleo con las cadenas dadas y una base nueva.
func nuevoCoreConCadenas(t *testing.T, ids ...string) *Core {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	chains := make([]chain.Chain, 0, len(ids))
	for _, id := range ids {
		chains = append(chains, cadenaConID{&fakeChain{products: map[string]chain.Product{}}, id})
	}
	return New(config.Config{}, chains, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// guardar registra un producto como match con precio para una lista.
func guardar(t *testing.T, c *Core, items []list.Item, precios map[string]map[string]float64) {
	t.Helper()
	ids, err := c.store.SyncItems(items)
	if err != nil {
		t.Fatal(err)
	}
	for nombre, porCadena := range precios {
		for chainID, precio := range porCadena {
			p := chain.Product{
				Chain: chainID, Name: nombre, URL: "https://x/" + chainID + "/" + nombre,
				Price: precio, Available: true, FetchedAt: time.Now(),
			}
			if err := c.store.SetMatch(ids[nombre], chainID, p, 0.9); err != nil {
				t.Fatal(err)
			}
			if err := c.store.InsertPrice(chainID, p); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestComparisonCadenaIncompletaNoGana cubre el caso que motivó el cambio: DÍA
// tiene 1,29 € solo del pan, pero como no tiene la leche no puede decirse que
// sea la más barata, porque con ella no harías la compra entera.
func TestComparisonCadenaIncompletaNoGana(t *testing.T) {
	items := []list.Item{{Name: "Leche 1L", Quantity: 1}, {Name: "Pan de molde", Quantity: 1}}
	c := nuevoCoreConCadenas(t, "mercadona", "dia")
	guardar(t, c, items, map[string]map[string]float64{
		"Leche 1L":     {"mercadona": 10.00},
		"Pan de molde": {"mercadona": 5.00, "dia": 1.29},
	})

	cmp, err := c.Comparison(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dia := cmp.CoverageOf("dia")
	if dia.Complete {
		t.Error("DÍA con 1 de 2 productos no puede estar completa")
	}
	if dia.Found != 1 || dia.Total != 2 {
		t.Errorf("cobertura de DÍA = %d de %d, want 1 de 2", dia.Found, dia.Total)
	}
	if len(dia.Missing) != 1 || dia.Missing[0] != "Leche 1L" {
		t.Errorf("faltantes = %v, want [Leche 1L]", dia.Missing)
	}
	mercadona := cmp.CoverageOf("mercadona")
	if !mercadona.Complete {
		t.Error("Mercadona tiene los dos productos y debería estar completa")
	}
	if cmp.CheapestChain != "mercadona" {
		t.Errorf("CheapestChain = %q, want mercadona (DÍA no cubre la lista)", cmp.CheapestChain)
	}
	if cmp.Totals["dia"] != 1.29 {
		t.Errorf("total parcial de DÍA = %v, want 1.29", cmp.Totals["dia"])
	}
	if !cmp.MixedComplete() {
		t.Error("la compra mixta sí cubre la lista: cada producto está en alguna cadena")
	}
}

// TestComparisonNingunaCadenaCompleta: si nadie tiene la lista entera, no se
// elige ninguna como más barata.
func TestComparisonNingunaCadenaCompleta(t *testing.T) {
	items := []list.Item{{Name: "Leche 1L", Quantity: 1}, {Name: "Pan de molde", Quantity: 1}}
	c := nuevoCoreConCadenas(t, "mercadona", "dia")
	guardar(t, c, items, map[string]map[string]float64{
		"Leche 1L":     {"mercadona": 10.00},
		"Pan de molde": {"dia": 1.29},
	})

	cmp, err := c.Comparison(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cmp.CheapestChain != "" {
		t.Errorf("CheapestChain = %q, debe quedar vacío si nadie cubre la lista", cmp.CheapestChain)
	}
	if cmp.MaxSaving != 0 {
		t.Errorf("MaxSaving = %v, want 0 sin cadenas completas", cmp.MaxSaving)
	}
	if cmp.CoverageOf("mercadona").Found != 1 || cmp.CoverageOf("dia").Found != 1 {
		t.Error("cada cadena debería tener 1 de 2")
	}
}

// TestComparisonProductoEnNingunaCadena: un producto que no está en ninguna
// cadena deja la compra mixta incompleta y se avisa.
func TestComparisonProductoEnNingunaCadena(t *testing.T) {
	items := []list.Item{{Name: "Leche 1L", Quantity: 1}, {Name: "Kéfir", Quantity: 1}}
	c := nuevoCoreConCadenas(t, "mercadona")
	guardar(t, c, items, map[string]map[string]float64{
		"Leche 1L": {"mercadona": 2.00},
	})

	cmp, err := c.Comparison(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cmp.MixedComplete() {
		t.Error("con un producto sin ninguna cadena la compra mixta no está completa")
	}
	if len(cmp.Missing) != 1 || cmp.Missing[0] != "Kéfir" {
		t.Errorf("Missing = %v, want [Kéfir]", cmp.Missing)
	}
	if cmp.CoverageOf("mercadona").Complete {
		t.Error("con el kéfir sin resolver Mercadona no cubre la lista")
	}
	if cmp.CheapestChain != "" {
		t.Errorf("CheapestChain = %q, want vacío", cmp.CheapestChain)
	}
}
