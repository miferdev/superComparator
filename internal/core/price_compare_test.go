package core

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/store"
)

func TestCheapestIndex(t *testing.T) {
	cases := []struct {
		name      string
		opts      []comparable
		want      int
		criterion string
	}{
		{
			name: "misma unidad: gana el menor €/kg aunque cueste más",
			opts: []comparable{
				{price: 2, measurePrice: 8, measureUnit: "kg", available: true},
				{price: 3, measurePrice: 6, measureUnit: "kg", available: true},
			},
			want:      1,
			criterion: criterionPerKg,
		},
		{
			name: "misma unidad: gana el menor €/l",
			opts: []comparable{
				{price: 2, measurePrice: 2.5, measureUnit: "l", available: true},
				{price: 1.8, measurePrice: 1.9, measureUnit: "l", available: true},
			},
			want:      1,
			criterion: criterionPerL,
		},
		{
			name: "kg frente a l no se mezclan: decide el total",
			opts: []comparable{
				{price: 2, measurePrice: 10, measureUnit: "kg", available: true},
				{price: 1.8, measurePrice: 2.5, measureUnit: "l", available: true},
			},
			want:      1,
			criterion: criterionTotal,
		},
		{
			name: "una sola con medida: decide el total",
			opts: []comparable{
				{price: 2, available: true},
				{price: 1.8, measurePrice: 2.5, measureUnit: "l", available: true},
			},
			want:      1,
			criterion: criterionTotal,
		},
		{
			name: "se descarta la no disponible aunque sea la más barata",
			opts: []comparable{
				{price: 0.5, measurePrice: 0.5, measureUnit: "l", available: false},
				{price: 1.8, measurePrice: 1.9, measureUnit: "l", available: true},
				{price: 2.2, measurePrice: 2.3, measureUnit: "l", available: true},
			},
			want:      1,
			criterion: criterionPerL,
		},
		{
			name: "sin opciones válidas",
			opts: []comparable{
				{price: 0, available: true},
				{price: 1, available: false},
			},
			want:      -1,
			criterion: "",
		},
	}
	for _, c := range cases {
		got, criterion := cheapestIndex(c.opts)
		if got != c.want || criterion != c.criterion {
			t.Errorf("%s: cheapestIndex = (%d, %q); want (%d, %q)", c.name, got, criterion, c.want, c.criterion)
		}
	}
}

type seedOption struct {
	chain        string
	price        float64
	measurePrice float64
	measureUnit  string
	available    bool
	fetchedAt    time.Time
}

func comparisonSeeded(t *testing.T, opts ...seedOption) Comparison {
	t.Helper()
	return comparisonSeededWith(t, 0, opts...)
}

func comparisonSeededWith(t *testing.T, maxAge time.Duration, opts ...seedOption) Comparison {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ids, err := st.SyncItems([]list.Item{{Name: "producto", Quantity: 2}})
	if err != nil {
		t.Fatalf("SyncItems: %v", err)
	}
	for _, o := range opts {
		fetched := o.fetchedAt
		if fetched.IsZero() {
			fetched = time.Now()
		}
		p := chain.Product{
			Chain: o.chain, URL: "https://ejemplo.test/" + o.chain, Name: "Producto",
			Price: o.price, MeasurePrice: o.measurePrice, MeasureUnit: o.measureUnit,
			Available: o.available, FetchedAt: fetched,
		}
		if err := st.SetMatch(ids["producto"], o.chain, p, 1); err != nil {
			t.Fatalf("SetMatch: %v", err)
		}
		if err := st.InsertPrice(o.chain, p); err != nil {
			t.Fatalf("InsertPrice: %v", err)
		}
	}
	c := New(config.Config{MaxPriceAge: maxAge}, nil, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cmp, err := c.Comparison(context.Background())
	if err != nil {
		t.Fatalf("Comparison: %v", err)
	}
	return cmp
}

func TestComparisonUsaPrecioPorMedida(t *testing.T) {
	cmp := comparisonSeeded(t,
		seedOption{chain: "mercadona", price: 2.00, measurePrice: 2.00, measureUnit: "l", available: true},
		seedOption{chain: "ahorramas", price: 1.80, measurePrice: 2.50, measureUnit: "l", available: true},
	)
	item := cmp.Items[0]
	if item.Cheapest != "mercadona" || item.Criterion != criterionPerL {
		t.Fatalf("comparativa = %+v", item)
	}
	if item.CheapestPrice != 2.00 {
		t.Fatalf("CheapestPrice = %v", item.CheapestPrice)
	}
	if diff := cmp.MixedTotal - 2.00*2; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("MixedTotal = %v", cmp.MixedTotal)
	}
}

func TestComparisonNoMezclaDimensiones(t *testing.T) {
	cmp := comparisonSeeded(t,
		seedOption{chain: "mercadona", price: 2.00, measurePrice: 10.00, measureUnit: "kg", available: true},
		seedOption{chain: "ahorramas", price: 1.80, measurePrice: 2.50, measureUnit: "l", available: true},
	)
	item := cmp.Items[0]
	if item.Cheapest != "ahorramas" || item.Criterion != criterionTotal {
		t.Fatalf("comparativa = %+v", item)
	}
}

func TestComparisonDescartaNoDisponible(t *testing.T) {
	cmp := comparisonSeeded(t,
		seedOption{chain: "mercadona", price: 0.50, measurePrice: 0.50, measureUnit: "l", available: false},
		seedOption{chain: "ahorramas", price: 1.80, measurePrice: 1.90, measureUnit: "l", available: true},
	)
	if cmp.Items[0].Cheapest != "ahorramas" {
		t.Fatalf("comparativa = %+v", cmp.Items[0])
	}
}

func TestComparisonDescartaPrecioCaducado(t *testing.T) {
	cmp := comparisonSeededWith(t, 24*time.Hour,
		seedOption{
			chain: "mercadona", price: 1.00, measurePrice: 1.00, measureUnit: "l",
			available: true, fetchedAt: time.Now().Add(-48 * time.Hour),
		},
		seedOption{chain: "ahorramas", price: 1.50, measurePrice: 1.50, measureUnit: "l", available: true},
	)
	item := cmp.Items[0]
	if item.Cheapest != "ahorramas" {
		t.Fatalf("debe descartar el precio caducado: %+v", item)
	}
	for _, opt := range item.Options {
		if opt.Chain == "mercadona" && !opt.Stale {
			t.Fatalf("el precio viejo debería marcarse como caducado: %+v", opt)
		}
	}
}
