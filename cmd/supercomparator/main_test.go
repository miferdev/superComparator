package main

import (
	"testing"

	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/store"
)

func match_(item string, score float64) store.Match {
	return store.Match{ItemName: item, Score: score}
}

func TestNeedsResolve(t *testing.T) {
	casos := []struct {
		nombre  string
		matches []store.Match
		items   []list.Item
		want    bool
	}{
		{
			nombre:  "lista vacía de matches",
			matches: nil,
			items:   []list.Item{{Name: "Leche", Quantity: 1}},
			want:    true,
		},
		{
			nombre:  "todo resuelto con buena confianza",
			matches: []store.Match{match_("Leche", 0.9), match_("Pan", 0.8)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}, {Name: "Pan", Quantity: 1}},
			want:    false,
		},
		{
			nombre:  "producto nuevo sin resolver aunque haya muchos matches",
			matches: []store.Match{match_("Leche", 0.9), match_("Pan", 0.8), match_("Kéfir", 0.7)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}, {Name: "Pan", Quantity: 1}, {Name: "Yogur", Quantity: 1}},
			want:    true,
		},
		{
			nombre:  "quedan ítems del histórico que ya no están en la lista",
			matches: []store.Match{match_("Leche", 0.9), match_("Pan", 0.8), match_("Antiguo", 0.9)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}, {Name: "Pan", Quantity: 1}},
			want:    true,
		},
		{
			nombre:  "coincidencia dudosa",
			matches: []store.Match{match_("Leche", 0.9), match_("Pan", match.AutoThreshold-0.1)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}, {Name: "Pan", Quantity: 1}},
			want:    true,
		},
	}
	for _, c := range casos {
		if got := needsResolve(c.matches, c.items, 2); got != c.want {
			t.Errorf("%s: needsResolve = %v, want %v", c.nombre, got, c.want)
		}
	}
}
