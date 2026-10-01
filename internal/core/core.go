// Package core orquesta la resolución de la lista y la comprobación de
// precios. Depende de los puertos (chain, store, match), nunca de los
// adaptadores concretos ni de la TUI, y comunica su avance con eventos.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	yaResueltos := c.resolvedChains()

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
			needsReview, ok := c.resolveItem(ctx, item, ids[item.Name], yaResueltos[item.Name], emit)
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

// resolvedChains indica, para cada producto, en qué cadenas ya hay un match con
// confianza suficiente. Permite que al añadir una cadena solo se resuelva lo que
// falta en ella, sin volver a descargar todo lo demás.
func (c *Core) resolvedChains() map[string]map[string]bool {
	matches, err := c.store.Matches()
	if err != nil {
		return nil
	}
	out := make(map[string]map[string]bool, len(matches))
	for _, m := range matches {
		if m.Score < match.AutoThreshold {
			continue
		}
		if out[m.ItemName] == nil {
			out[m.ItemName] = make(map[string]bool, len(c.order))
		}
		out[m.ItemName][m.Chain] = true
	}
	return out
}

func (c *Core) resolveItem(ctx context.Context, item list.Item, itemID int64, yaResueltos map[string]bool, emit func(Event)) (needsReview bool, ok bool) {
	any := false
	for _, chainID := range c.order {
		if ctx.Err() != nil {
			break
		}
		if yaResueltos[chainID] {
			any = true
			continue
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
	// Primero se borra lo que hubiera: al volver a resolver, o queda el
	// producto nuevo o no queda nada. Un match que sobreviviera sería de una
	// versión anterior de las reglas y podría no ser lo que se pidió.
	if err := c.store.ClearMatch(itemID, chainID); err != nil {
		return fmt.Errorf("limpiando match anterior: %w", err)
	}
	descargados, err := c.descargarCandidatos(ctx, item.Name, chainID)
	if err != nil {
		return err
	}
	candidatos, alternativas := evaluarCandidatos(item.Name, descargados)
	if len(candidatos) == 0 {
		if len(alternativas) == 0 {
			return errors.New("ningún candidato disponible")
		}
		return errors.New("ningún candidato encaja con lo pedido")
	}

	mejor, ok := elegirCandidato(candidatos)
	if !ok {
		// Una coincidencia dudosa no entra en la comparativa: un precio de un
		// producto que no es el pedido falsearía el total. Se guarda igualmente
		// como alternativa para que el informe la muestre y decida el usuario.
		if err := c.store.ReplaceAlternatives(itemID, chainID, alternativas); err != nil {
			return fmt.Errorf("guardando alternativas: %w", err)
		}
		c.emit(emit, ItemNeedsReview{Item: item.Name})
		return fmt.Errorf("sin coincidencia suficiente (%.2f): %s", mejor.score, mejor.product.Name)
	}

	if err := c.store.ReplaceAlternatives(itemID, chainID, alternativas); err != nil {
		return fmt.Errorf("guardando alternativas: %w", err)
	}
	if err := c.store.SetMatch(itemID, chainID, mejor.product, mejor.score); err != nil {
		return fmt.Errorf("guardando match: %w", err)
	}
	if err := c.store.InsertPrice(chainID, mejor.product); err != nil {
		return fmt.Errorf("guardando precio: %w", err)
	}
	c.log.Info("match resuelto",
		"item", item.Name,
		"chain", chainID,
		"product", mejor.product.Name,
		"score", mejor.score,
		"price", mejor.product.Price,
		"measure_price", mejor.product.MeasurePrice,
		"measure_unit", mejor.product.MeasureUnit,
	)
	c.emit(emit, ChainResolved{Item: item.Name, Chain: chainID, Product: mejor.product, Score: mejor.score, Alternatives: alternativas})
	return nil
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
