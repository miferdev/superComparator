// Package core orquesta la resolución de la lista y la comprobación de
// precios. Depende de los puertos (chain, store, match), nunca de los
// adaptadores concretos ni de la TUI, y comunica su avance con eventos.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/store"
)

type Core struct {
	cfg    config.Config
	chains map[string]chain.Chain
	order  []string
	store  *store.Store
	log    *slog.Logger

	emitMu    sync.Mutex
	sitemapMu sync.Mutex
	sitemaps  map[string][]chain.SitemapEntry
}

func New(cfg config.Config, chains []chain.Chain, st *store.Store, log *slog.Logger) *Core {
	c := &Core{
		cfg:      cfg,
		chains:   make(map[string]chain.Chain, len(chains)),
		store:    st,
		log:      log,
		sitemaps: make(map[string][]chain.SitemapEntry),
	}
	for _, ch := range chains {
		c.chains[ch.ID()] = ch
		c.order = append(c.order, ch.ID())
	}
	return c
}

func (c *Core) Store() *store.Store { return c.store }

func (c *Core) Chains() []string { return c.order }

func (c *Core) emit(emit func(Event), e Event) {
	if emit == nil {
		return
	}
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	emit(e)
}

func (c *Core) sitemap(ctx context.Context, id string) ([]chain.SitemapEntry, error) {
	c.sitemapMu.Lock()
	defer c.sitemapMu.Unlock()
	if entries, ok := c.sitemaps[id]; ok {
		return entries, nil
	}
	ch, ok := c.chains[id]
	if !ok {
		return nil, fmt.Errorf("cadena desconocida: %s", id)
	}
	entries, err := ch.Sitemap(ctx)
	if err != nil {
		return nil, err
	}
	c.sitemaps[id] = entries
	return entries, nil
}

// Resolve busca en cada cadena el producto que mejor encaja con cada línea
// de la lista y guarda el match y su precio.
func (c *Core) Resolve(ctx context.Context, items []list.Item, emit func(Event)) error {
	ids, err := c.store.SyncItems(items)
	if err != nil {
		return err
	}
	c.emit(emit, RunStarted{Phase: "resolver", Total: len(items)})

	var mu sync.Mutex
	resolved, review, failed := 0, 0, 0
	sem := make(chan struct{}, c.cfg.Workers)
	var wg sync.WaitGroup
	for idx, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, item list.Item) {
			defer wg.Done()
			defer func() { <-sem }()
			c.emit(emit, ItemStarted{Item: item.Name, Index: idx + 1, Total: len(items)})
			needsReview, ok := c.resolveItem(ctx, item, ids[item.Name], emit)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case !ok:
				failed++
			case needsReview:
				review++
			default:
				resolved++
			}
		}(idx, item)
	}
	wg.Wait()

	c.emit(emit, RunFinished{Phase: "resolver", Resolved: resolved, Review: review, Failed: failed, Err: ctx.Err()})
	return ctx.Err()
}

func (c *Core) resolveItem(ctx context.Context, item list.Item, itemID int64, emit func(Event)) (needsReview bool, ok bool) {
	any := false
	for _, chainID := range c.order {
		if ctx.Err() != nil {
			break
		}
		if err := c.resolveItemInChain(ctx, item, itemID, chainID, emit); err != nil {
			c.emit(emit, ItemFailed{Item: item.Name, Chain: chainID, Err: err.Error()})
			continue
		}
		any = true
	}
	if !any {
		return false, false
	}
	return c.itemNeedsReview(item.Name), true
}

func (c *Core) resolveItemInChain(ctx context.Context, item list.Item, itemID int64, chainID string, emit func(Event)) error {
	ch := c.chains[chainID]
	entries, err := c.sitemap(ctx, chainID)
	if err != nil {
		return fmt.Errorf("sitemap: %w", err)
	}
	pool, maxCandidates := c.candidateLimits()
	ranked := match.Rank(item.Name, entries, pool)
	if len(ranked) == 0 {
		return errors.New("sin candidatos en el sitemap")
	}

	type candidate struct {
		product chain.Product
		score   float64
	}
	var candidates []candidate
	alternatives := make([]Alternative, 0, len(ranked))
	fetched := 0
	for _, cand := range ranked {
		if len(candidates) >= maxCandidates || fetched >= pool {
			break
		}
		if cand.Score < c.cfg.MinScore {
			continue
		}
		if fetched > 0 {
			time.Sleep(c.cfg.Delay)
		}
		fetched++
		p, err := ch.Fetch(ctx, cand.Entry.URL)
		if err != nil {
			c.log.Debug("candidato descartado", "url", cand.Entry.URL, "err", err)
			continue
		}
		if !p.Available || p.Price <= 0 {
			continue
		}
		score := match.Similarity(item.Name, p.Name, p.Format)
		alternatives = append(alternatives, Alternative{
			URL: p.URL, Name: p.Name, Score: score,
			Price: p.Price, MeasurePrice: p.MeasurePrice, MeasureUnit: p.MeasureUnit,
		})
		candidates = append(candidates, candidate{product: p, score: score})
	}
	if len(candidates) == 0 {
		return errors.New("ningún candidato disponible")
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	// Si hay opciones que encajan bien, se muestra la más barata de ellas
	// aplicando la misma regla que en la comparativa.
	eligible := make([]comparable, 0, len(candidates))
	indexes := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if c.score < match.AutoThreshold {
			continue
		}
		eligible = append(eligible, comparableOfProduct(c.product))
		indexes = append(indexes, i)
	}
	if k, _ := cheapestIndex(eligible); k >= 0 {
		best = candidates[indexes[k]]
	}

	sort.Slice(alternatives, func(i, j int) bool { return alternatives[i].Score > alternatives[j].Score })

	if err := c.store.SetMatch(itemID, chainID, best.product, best.score); err != nil {
		return fmt.Errorf("guardando match: %w", err)
	}
	if err := c.store.InsertPrice(chainID, best.product); err != nil {
		return fmt.Errorf("guardando precio: %w", err)
	}
	c.log.Info("match resuelto",
		"item", item.Name,
		"chain", chainID,
		"product", best.product.Name,
		"score", best.score,
		"price", best.product.Price,
		"measure_price", best.product.MeasurePrice,
		"measure_unit", best.product.MeasureUnit,
	)
	if best.score < match.AutoThreshold {
		c.log.Warn("match con confianza baja",
			"item", item.Name,
			"chain", chainID,
			"product", best.product.Name,
			"score", best.score,
		)
	}
	c.emit(emit, ChainResolved{Item: item.Name, Chain: chainID, Product: best.product, Score: best.score, Alternatives: alternatives})
	if best.score < match.AutoThreshold {
		c.emit(emit, ItemNeedsReview{Item: item.Name})
	}
	return nil
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

// Check revisa los precios de los productos ya vinculados y avisa de cambios
// y descatalogados.
func (c *Core) Check(ctx context.Context, emit func(Event)) error {
	matches, err := c.store.Matches()
	if err != nil {
		return err
	}
	c.emit(emit, RunStarted{Phase: "comprobar", Total: len(matches)})

	var mu sync.Mutex
	changed, delisted, failed := 0, 0, 0
	sem := make(chan struct{}, c.cfg.Workers)
	var wg sync.WaitGroup
	for idx, m := range matches {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, m store.Match) {
			defer wg.Done()
			defer func() { <-sem }()
			c.emit(emit, ItemStarted{Item: m.ItemName, Index: idx + 1, Total: len(matches)})
			ch, okCh := c.chains[m.Chain]
			if !okCh {
				return
			}
			p, err := ch.Fetch(ctx, m.URL)
			time.Sleep(c.cfg.Delay)
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, chain.ErrNotFound) {
				if markErr := c.store.SetMatchAvailable(m.ItemID, m.Chain, false); markErr != nil {
					c.log.Debug("marcando no disponible", "item", m.ItemName, "err", markErr)
				}
				c.emit(emit, ProductDelisted{Item: m.ItemName, Chain: m.Chain})
				delisted++
				return
			}
			if err != nil {
				c.emit(emit, ItemFailed{Item: m.ItemName, Chain: m.Chain, Err: err.Error()})
				failed++
				return
			}
			if !m.Available {
				if markErr := c.store.SetMatchAvailable(m.ItemID, m.Chain, true); markErr != nil {
					c.log.Debug("marcando disponible", "item", m.ItemName, "err", markErr)
				}
			}
			if prev, okPrev, _ := c.store.LatestPrice(m.Chain, m.URL); okPrev && prev.Price != p.Price {
				c.emit(emit, PriceChanged{Item: m.ItemName, Chain: m.Chain, Old: prev.Price, New: p.Price})
				changed++
			}
			if err := c.store.InsertPrice(m.Chain, p); err != nil {
				failed++
			}
		}(idx, m)
	}
	wg.Wait()

	c.emit(emit, RunFinished{Phase: "comprobar", Resolved: changed, Review: delisted, Failed: failed, Err: ctx.Err()})
	return ctx.Err()
}

type ChainOption struct {
	Chain        string
	Product      string
	Format       string
	URL          string
	Price        float64
	MeasurePrice float64
	MeasureUnit  string
	OldPrice     float64
	Promo        bool
	Available    bool
	FetchedAt    time.Time
	Stale        bool
}

type ItemComparison struct {
	Name          string
	Quantity      int
	Options       []ChainOption
	Cheapest      string
	CheapestPrice float64
	Criterion     string
	Save          float64
}

type Comparison struct {
	Items         []ItemComparison
	Chains        []string
	Totals        map[string]float64
	MixedTotal    float64
	CheapestChain string
	MaxSaving     float64
	GeneratedAt   time.Time
}

// Comparison calcula el total de la compra en cada cadena y la mejor compra
// mixta usando los últimos precios guardados.
func (c *Core) Comparison(_ context.Context) (Comparison, error) {
	rows, err := c.store.LastMatchPrices()
	if err != nil {
		return Comparison{}, err
	}
	cmp := Comparison{
		Chains:      append([]string(nil), c.order...),
		Totals:      make(map[string]float64, len(c.order)),
		GeneratedAt: time.Now(),
	}
	index := make(map[string]*ItemComparison)
	for _, row := range rows {
		ic, ok := index[row.ItemName]
		if !ok {
			cmp.Items = append(cmp.Items, ItemComparison{Name: row.ItemName, Quantity: row.Quantity})
			ic = &cmp.Items[len(cmp.Items)-1]
			index[row.ItemName] = ic
		}
		ic.Options = append(ic.Options, ChainOption{
			Chain:        row.Chain,
			Product:      row.MatchedName,
			Format:       row.Format,
			URL:          row.URL,
			Price:        row.Price,
			MeasurePrice: row.MeasurePrice,
			MeasureUnit:  row.MeasureUnit,
			OldPrice:     row.OldPrice,
			Promo:        row.OldPrice > row.Price && row.Price > 0,
			Available:    row.Available,
			FetchedAt:    row.FetchedAt,
			Stale:        c.priceIsStale(row.FetchedAt),
		})
	}
	for i := range cmp.Items {
		ic := &cmp.Items[i]
		for _, opt := range ic.Options {
			if opt.Price > 0 {
				cmp.Totals[opt.Chain] += opt.Price * float64(ic.Quantity)
			}
		}
		comps := make([]comparable, len(ic.Options))
		for j, opt := range ic.Options {
			comps[j] = comparableOfOption(opt)
		}
		if k, criterion := cheapestIndex(comps); k >= 0 {
			ic.Criterion = criterion
			ic.Cheapest = ic.Options[k].Chain
			ic.CheapestPrice = ic.Options[k].Price
			cmp.MixedTotal += ic.Options[k].Price * float64(ic.Quantity)
			if next, ok := runnerUpPrice(comps, k); ok {
				ic.Save = (next - ic.Options[k].Price) * float64(ic.Quantity)
			}
		}
	}
	best := ""
	for _, chainID := range c.order {
		total, ok := cmp.Totals[chainID]
		if !ok || total == 0 {
			continue
		}
		if best == "" || total < cmp.Totals[best] {
			best = chainID
		}
	}
	cmp.CheapestChain = best
	if best != "" {
		for chainID, total := range cmp.Totals {
			if chainID != best && total > cmp.Totals[best] {
				if saving := total - cmp.Totals[best]; saving > cmp.MaxSaving {
					cmp.MaxSaving = saving
				}
			}
		}
	}
	return cmp, nil
}

// ChooseAlternative fija manualmente otro producto para un ítem (desde la TUI).
func (c *Core) ChooseAlternative(ctx context.Context, itemName, chainID, url string) (chain.Product, error) {
	ch, ok := c.chains[chainID]
	if !ok {
		return chain.Product{}, fmt.Errorf("cadena desconocida: %s", chainID)
	}
	p, err := ch.Fetch(ctx, url)
	if err != nil {
		return chain.Product{}, err
	}
	matches, err := c.store.Matches()
	if err != nil {
		return chain.Product{}, err
	}
	var itemID int64
	for _, m := range matches {
		if m.ItemName == itemName {
			itemID = m.ItemID
			break
		}
	}
	if itemID == 0 {
		return chain.Product{}, fmt.Errorf("ítem no encontrado: %s", itemName)
	}
	score := match.Similarity(itemName, p.Name, p.Format)
	if err := c.store.SetMatch(itemID, chainID, p, score); err != nil {
		return chain.Product{}, err
	}
	if err := c.store.InsertPrice(chainID, p); err != nil {
		return chain.Product{}, err
	}
	return p, nil
}
