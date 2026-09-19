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
