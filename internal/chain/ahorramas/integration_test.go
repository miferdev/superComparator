//go:build integration

package ahorramas

import (
	"context"
	"testing"
	"time"
)

func TestSitemapLive(t *testing.T) {
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	entries, err := c.Sitemap(ctx)
	if err != nil {
		t.Fatalf("sitemap: %v", err)
	}
	if len(entries) < 1000 {
		t.Fatalf("entradas = %d, se esperaban miles", len(entries))
	}
}

func TestFetchLive(t *testing.T) {
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p, err := c.Fetch(ctx, "https://www.ahorramas.com/garbanzo-cocido-luengo-400g-44569.html")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if p.Name == "" || p.Price <= 0 {
		t.Fatalf("producto incompleto: %+v", p)
	}
}
