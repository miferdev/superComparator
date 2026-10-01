//go:build integration

package dia

import (
	"context"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/match"
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
	p, err := c.Fetch(ctx, "https://www.dia.es/huevos-leche-y-mantequilla/leche/p/16065")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if p.Name == "" || p.Price <= 0 {
		t.Fatalf("producto incompleto: %+v", p)
	}
	if p.MeasureUnit == "" {
		t.Errorf("no se ha extraído el precio por unidad: %+v", p)
	}
}

// TestLookupLive comprueba que la búsqueda por categoría devuelve fichas
// plausibles para una consulta de la compra.
func TestLookupLive(t *testing.T) {
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cands, err := c.Lookup(ctx, "Leche entera 1L")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("no se han obtenido candidatos")
	}
	var mejor string
	var mejorScore float64
	for _, cand := range cands[:min(3, len(cands))] {
		p, err := c.Fetch(ctx, cand.URL)
		if err != nil {
			t.Logf("descartado %s: %v", cand.URL, err)
			continue
		}
		if s := match.Similarity("Leche entera 1L", p.Name, p.Format); s > mejorScore {
			mejor, mejorScore = p.Name, s
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Logf("mejor coincidencia: %.2f %s", mejorScore, mejor)
	if mejorScore < 0.5 {
		t.Errorf("ningún candidato superó el umbral: mejor %.2f (%s)", mejorScore, mejor)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
