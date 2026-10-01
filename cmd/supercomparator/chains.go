package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/chain/ahorramas"
	"github.com/miferdev/superComparator/internal/chain/alcampo"
	"github.com/miferdev/superComparator/internal/chain/dia"
	"github.com/miferdev/superComparator/internal/chain/mercadona"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/store"
)

func loadConfig(f *flags) config.Config {
	cfg := config.Load()
	if f.cp != "" {
		cfg.PostalCode = f.cp
	}
	if f.lista != "" {
		cfg.ListaPath = f.lista
	}
	if f.db != "" {
		cfg.DBPath = f.db
		if f.reportPath == "" {
			cfg.ReportPath = filepath.Join(filepath.Dir(cfg.DBPath), "informe.md")
		}
	}
	if f.reportPath != "" {
		cfg.ReportPath = f.reportPath
	}
	if f.browserBin != "" {
		cfg.BrowserBin = f.browserBin
	}
	if f.chains != "" {
		cfg.Chains = config.SplitChains(f.chains)
	}
	if f.workers > 0 {
		cfg.Workers = f.workers
	}
	if f.candidates > 0 {
		cfg.Candidates = f.candidates
	}
	if f.delayMS > 0 {
		cfg.Delay = time.Duration(f.delayMS) * time.Millisecond
	}
	return cfg
}

func build(cfg config.Config) (*core.Core, func(), error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	chains, err := selectChains(cfg)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	c := core.New(cfg, chains, st, loggerFor(cfg))
	cleanup := func() {
		closeChains(chains)
		st.Close()
	}
	return c, cleanup, nil
}

// closeChains libera los navegadores headless que hayan abierto los adaptadores.
func closeChains(chains []chain.Chain) {
	for _, ch := range chains {
		if c, ok := ch.(interface{ Close() }); ok {
			c.Close()
		}
	}
}

// selectChains monta las cadenas pedidas. Alcampo viene desactivada porque su
// WAF no responde a los clientes automatizados; se activa con --cadenas.
func selectChains(cfg config.Config) ([]chain.Chain, error) {
	mc := mercadona.New(cfg)
	all := map[string]chain.Chain{
		"mercadona": mc,
		"ahorramas": ahorramas.New(),
		"dia":       dia.New(),
		"alcampo":   alcampo.New(cfg),
	}
	pedidas := cfg.Chains
	if len(pedidas) == 0 {
		pedidas = defaultChains
	}
	var out []chain.Chain
	for _, name := range pedidas {
		ch, ok := all[name]
		if !ok {
			return nil, fmt.Errorf("cadena desconocida: %s (disponibles: %s)", name, strings.Join(chainNames(all), ", "))
		}
		out = append(out, ch)
	}
	return out, nil
}

func chainNames(all map[string]chain.Chain) []string {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
