package core

import (
	"context"
	"fmt"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/match"
)

// candidates devuelve los productos que mejor encajan con la consulta. Si la
// cadena ofrece búsqueda propia se le pregunta a ella; si no, se rankea el
// sitemap completo.
func (c *Core) candidates(ctx context.Context, query, chainID string) ([]match.Scored, error) {
	if lk, ok := c.chains[chainID].(chain.Lookup); ok {
		entries, err := lk.Lookup(ctx, query)
		if err != nil {
			c.log.Debug("buscador sin resultado, se usa el sitemap", "chain", chainID, "err", err)
		} else if len(entries) > 0 {
			pool, _ := c.candidateLimits()
			if len(entries) > pool {
				entries = entries[:pool]
			}
			return match.InOrder(entries), nil
		}
	}
	entries, err := c.sitemap(ctx, chainID)
	if err != nil {
		return nil, fmt.Errorf("sitemap: %w", err)
	}
	pool, _ := c.candidateLimits()
	return match.Rank(query, entries, pool), nil
}

// priceIsStale indica si el precio superó la antigüedad máxima configurada.
func (c *Core) priceIsStale(fetchedAt time.Time) bool {
	if c.cfg.MaxPriceAge <= 0 || fetchedAt.IsZero() {
		return false
	}
	return time.Since(fetchedAt) > c.cfg.MaxPriceAge
}

// candidateLimits resuelve los topes de ranking y descarga, aplicando valores
// por defecto si la configuración llega a cero (tests o uso embebido).
func (c *Core) candidateLimits() (pool, maxCandidates int) {
	pool = c.cfg.RankPool
	if pool <= 0 {
		pool = c.cfg.Candidates
	}
	if pool <= 0 {
		pool = 3
	}
	maxCandidates = c.cfg.Candidates
	if maxCandidates <= 0 || maxCandidates > pool {
		maxCandidates = pool
	}
	return pool, maxCandidates
}

func (c *Core) itemNeedsReview(name string) bool {
	matches, err := c.store.Matches()
	if err != nil {
		return false
	}
	for _, m := range matches {
		if m.ItemName == name && m.Score < match.AutoThreshold {
			return true
		}
	}
	return false
}
