//go:build integration

package mercadona

import (
	"context"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/match"
)

func TestSitemapLive(t *testing.T) {
	c := New(config.Load())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	entries, err := c.Sitemap(ctx)
	if err != nil {
		t.Fatalf("sitemap: %v", err)
	}
	if len(entries) < 1000 {
		t.Fatalf("entradas = %d, se esperaban miles", len(entries))
	}
	if got := match.Rank("leche entera 1l", entries, 3); len(got) == 0 {
		t.Fatal("sin candidatos para 'leche entera 1l'")
	}
}

func TestFetchLive(t *testing.T) {
	cfg := config.Load()
	cfg.Timeout = 60 * time.Second
	c := New(cfg)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	p, err := c.Fetch(ctx, "https://tienda.mercadona.es/product/10005/chocolate-liquido-taza-hacendado-brick")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if p.Name == "" || p.Price <= 0 {
		t.Fatalf("producto incompleto: %+v", p)
	}
}
