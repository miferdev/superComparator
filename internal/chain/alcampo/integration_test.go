//go:build integration

package alcampo

import (
	"context"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/config"
)

func TestSitemapLive(t *testing.T) {
	c := New(config.Config{Timeout: 60 * time.Second})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	entries, err := c.Sitemap(ctx)
	if err != nil {
		t.Fatalf("sitemap: %v", err)
	}
	if len(entries) < 10000 {
		t.Fatalf("entradas = %d, se esperaban decenas de miles", len(entries))
	}
}

// TestFetchLive falla hoy con 403: el WAF de Alcampo bloquea las fichas a los
// clientes automatizados. Se mantiene el test para que en cuanto la web lo
// permita se vea al instante.
func TestFetchLive(t *testing.T) {
	c := New(config.Config{Timeout: 45 * time.Second})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	p, err := c.Fetch(ctx, "https://www.compraonline.alcampo.es/products/pan-de-trigo-de-espelta-64-400g/56004")
	if err != nil {
		t.Skipf("Alcampo no sirve la ficha a este cliente: %v", err)
	}
	if p.Name == "" || p.Price <= 0 {
		t.Fatalf("producto incompleto: %+v", p)
	}
}
