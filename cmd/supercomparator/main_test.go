package main

import (
	"strings"
	"testing"

	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/store"
)

func match_(item string, score float64) store.Match {
	return store.Match{ItemName: item, Score: score}
}

func enCadena(item, chain string, score float64) store.Match {
	m := match_(item, score)
	m.Chain = chain
	return m
}

var dosCadenas = []string{"mercadona", "ahorramas"}

func TestNeedsResolve(t *testing.T) {
	casos := []struct {
		nombre  string
		matches []store.Match
		items   []list.Item
		chains  []string
		want    bool
	}{
		{
			nombre: "lista vacía de matches",
			items:  []list.Item{{Name: "Leche", Quantity: 1}},
			chains: dosCadenas,
			want:   true,
		},
		{
			nombre:  "todo resuelto en todas las cadenas",
			matches: []store.Match{enCadena("Leche", "mercadona", 0.9), enCadena("Leche", "ahorramas", 0.8), enCadena("Pan", "mercadona", 0.8), enCadena("Pan", "ahorramas", 0.7)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}, {Name: "Pan", Quantity: 1}},
			chains:  dosCadenas,
			want:    false,
		},
		{
			nombre:  "falta la cadena nueva para un producto ya resuelto",
			matches: []store.Match{enCadena("Leche", "mercadona", 0.9), enCadena("Leche", "ahorramas", 0.8)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}},
			chains:  []string{"mercadona", "ahorramas", "dia"},
			want:    true,
		},
		{
			nombre:  "producto nuevo sin resolver aunque haya muchos matches",
			matches: []store.Match{enCadena("Leche", "mercadona", 0.9), enCadena("Leche", "ahorramas", 0.8), enCadena("Kéfir", "mercadona", 0.7), enCadena("Kéfir", "ahorramas", 0.7)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}, {Name: "Yogur", Quantity: 1}},
			chains:  dosCadenas,
			want:    true,
		},
		{
			nombre:  "quedan ítems del histórico que ya no están en la lista",
			matches: []store.Match{enCadena("Leche", "mercadona", 0.9), enCadena("Leche", "ahorramas", 0.9), enCadena("Antiguo", "mercadona", 0.9), enCadena("Antiguo", "ahorramas", 0.9)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}},
			chains:  dosCadenas,
			want:    true,
		},
		{
			nombre:  "coincidencia dudosa",
			matches: []store.Match{enCadena("Leche", "mercadona", 0.9), enCadena("Leche", "ahorramas", match.AutoThreshold-0.1)},
			items:   []list.Item{{Name: "Leche", Quantity: 1}},
			chains:  dosCadenas,
			want:    true,
		},
	}
	for _, c := range casos {
		if got := needsResolve(c.matches, c.items, c.chains); got != c.want {
			t.Errorf("%s: needsResolve = %v, want %v", c.nombre, got, c.want)
		}
	}
}

func TestSelectChains(t *testing.T) {
	casos := []struct {
		nombre string
		cfg    config.Config
		want   []string
		err    bool
	}{
		{nombre: "por defecto", cfg: config.Config{}, want: []string{"mercadona", "ahorramas", "dia"}},
		{nombre: "explícita", cfg: config.Config{Chains: []string{"dia", "mercadona"}}, want: []string{"dia", "mercadona"}},
		{nombre: "con alcampo", cfg: config.Config{Chains: []string{"mercadona", "alcampo"}}, want: []string{"mercadona", "alcampo"}},
		{nombre: "desconocida", cfg: config.Config{Chains: []string{"carrefour"}}, err: true},
	}
	for _, c := range casos {
		chains, err := selectChains(c.cfg)
		if c.err {
			if err == nil {
				t.Errorf("%s: esperaba error", c.nombre)
			}
			closeChains(chains)
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.nombre, err)
			continue
		}
		var got []string
		for _, ch := range chains {
			got = append(got, ch.ID())
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: cadenas = %v, want %v", c.nombre, got, c.want)
		}
		closeChains(chains)
	}
}

func TestSelectChainsAlcampoNoVienePorDefecto(t *testing.T) {
	chains, err := selectChains(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeChains(chains)
	for _, ch := range chains {
		if ch.ID() == "alcampo" {
			t.Error("Alcampo no debe compararse por defecto: su WAF bloquea las fichas")
		}
	}
}
