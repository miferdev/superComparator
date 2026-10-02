// Package catalog construye y consulta el catálogo de productos de todas las
// cadenas. El indexador lee los sitemaps, que es la parte barata, y deja cada
// producto en la base listo para que la cola de precios lo complete.
package catalog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/store"
)

// IndexerOptions afina el indexado de una cadena.
type IndexerOptions struct {
	// MaxProducts corta la indexación para probar. 0 = sin límite.
	MaxProducts int
	// OnProgress se llama cada lote con lo hecho hasta el momento.
	OnProgress func(chainID string, hechos int, total int)
}

// Indexer mete el catálogo de las cadenas en la base de datos.
type Indexer struct {
	chains []chain.Chain
	store  *store.Store
	log    *slog.Logger
	opts   IndexerOptions
}

func NewIndexer(chains []chain.Chain, st *store.Store, log *slog.Logger) *Indexer {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Indexer{chains: chains, store: st, log: log}
}

// WithOptions devuelve el indexador con las opciones indicadas.
func (ix *Indexer) WithOptions(opts IndexerOptions) *Indexer {
	ix.opts = opts
	return ix
}

// IndexCatalog indexa todas las cadenas. Una cadena que falle no impide que las
// demás terminen: el error se anota en su crawl_state y se sigue.
func (ix *Indexer) IndexCatalog(ctx context.Context) error {
	for _, ch := range ix.chains {
		if err := ix.IndexChain(ctx, ch); err != nil {
			ix.log.Warn("cadena no indexada", "chain", ch.ID(), "err", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	return nil
}

// IndexChain indexa el catálogo de una cadena: lee su sitemap, guarda nombre,
// medida, categoría y enlace de cada producto, y encola los que no tengan
// precio para que la cola de precios los visite.
func (ix *Indexer) IndexChain(ctx context.Context, ch chain.Chain) error {
	chainID := ch.ID()
	runID, err := ix.store.StartRun("catalogo", chainID)
	if err != nil {
		return err
	}

	if err := ix.store.SetCrawlState(store.CrawlState{
		Chain:     chainID,
		Phase:     "leyendo sitemap",
		Paused:    false,
		UpdatedAt: time.Now(),
	}); err != nil {
		return err
	}

	entries, err := ch.Sitemap(ctx)
	if err != nil {
		return ix.failChain(runID, chainID, "leyendo sitemap", err)
	}
	if ix.opts.MaxProducts > 0 && len(entries) > ix.opts.MaxProducts {
		entries = entries[:ix.opts.MaxProducts]
	}

	total, insertados, actualizados := 0, 0, 0
	lote := make([]store.Product, 0, 500)
	flush := func() error {
		if len(lote) == 0 {
			return nil
		}
		ins, upd, err := ix.store.UpsertProducts(chainID, lote)
		if err != nil {
			return err
		}
		insertados += ins
		actualizados += upd
		lote = lote[:0]
		return nil
	}

	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			_ = ix.store.SetCrawlState(store.CrawlState{
				Chain: chainID, Phase: "interrumpido", Cursor: e.URL,
				Done: total, Total: len(entries), UpdatedAt: time.Now(),
			})
			return err
		}
		p := productFromEntry(chainID, e)
		if p.Name == "" || p.URL == "" {
			continue
		}
		lote = append(lote, p)
		total++
		if len(lote) >= cap(lote) {
			if err := flush(); err != nil {
				return ix.failChain(runID, chainID, "guardando productos", err)
			}
			ix.report(chainID, total, len(entries))
		}
		if (i+1)%5000 == 0 {
			ix.log.Info("indexando", "chain", chainID, "hechos", total, "total", len(entries))
		}
	}
	if err := flush(); err != nil {
		return ix.failChain(runID, chainID, "guardando productos", err)
	}

	// Una cadena con los precios apagados no se encola: sus 89.615 fichas de
	// Alcampo darían 403 y solo llenarían la cola de trabajo inútil.
	encolados := 0
	cfg, ok, err := ix.store.Chain(chainID)
	if err != nil {
		return ix.failChain(runID, chainID, "leyendo la configuración", err)
	}
	if ok && !cfg.PreciosActivos {
		ix.log.Info("precios desactivados para esta cadena, no se encola", "chain", chainID)
	} else {
		encolados, err = ix.store.EnqueuePrices(chainID, 0)
		if err != nil {
			return ix.failChain(runID, chainID, "encolando precios", err)
		}
	}

	if err := ix.store.SetCrawlState(store.CrawlState{
		Chain:     chainID,
		Phase:     "indexado",
		Cursor:    "",
		Done:      total,
		Total:     total,
		Paused:    false,
		UpdatedAt: time.Now(),
	}); err != nil {
		return err
	}
	note := fmt.Sprintf("%d insertados, %d actualizados, %d en cola", insertados, actualizados, encolados)
	if err := ix.store.FinishRun(runID, total, 0, note); err != nil {
		return err
	}
	ix.log.Info("catálogo indexado", "chain", chainID, "productos", total, "nuevos", insertados, "en cola", encolados)
	ix.report(chainID, total, total)
	return nil
}

func (ix *Indexer) report(chainID string, hechos, total int) {
	if ix.opts.OnProgress != nil {
		ix.opts.OnProgress(chainID, hechos, total)
	}
}

func (ix *Indexer) failChain(runID int64, chainID, fase string, cause error) error {
	msg := fmt.Sprintf("%s: %v", fase, cause)
	if err := ix.store.SetCrawlState(store.CrawlState{
		Chain: chainID, Phase: "error", Paused: true, UpdatedAt: time.Now(),
	}); err != nil {
		ix.log.Debug("anotando error de cadena", "chain", chainID, "err", err)
	}
	if err := ix.store.FinishRun(runID, 0, 1, msg); err != nil {
		ix.log.Debug("cerrando run con error", "err", err)
	}
	return fmt.Errorf("indexando %s: %w", chainID, cause)
}

// SetFichaDatos actualiza el nombre y la categoría de un producto con los que
// publica su propia ficha. Hace falta sobre todo para DÍA, cuyo sitemap solo
// trae la categoría: sin esto, sus productos se llaman «leche» y no aparecen
// nunca al buscar «leche entera».
//
// search_name se recalcula con los mismos tokens que usa el indexador para no
// romper la búsqueda: si se quedara con los del slug, el nombre real no se
// encontraría. Solo escribe lo que venga informado, para no degradar lo bueno
// que ya había.
func (c *Catalog) SetFichaDatos(productID int64, name, category string) error {
	nombre := strings.TrimSpace(name)
	cat := strings.TrimSpace(category)
	if nombre == "" && cat == "" {
		return nil
	}
	if _, _, err := c.store.FichaData(productID, nombre, searchNameFor(nombre), cat); err != nil {
		return err
	}
	return nil
}

// searchNameFor es el nombre normalizado que se guarda para poder buscar.
func searchNameFor(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.Join(match.Tokens(name), " ")
}

// productFromEntry convierte una entrada de sitemap en un producto del catálogo.
// La medida sale del texto del nombre, que es donde la traen las cuatro cadenas.
func productFromEntry(chainID string, e chain.SitemapEntry) store.Product {
	name := strings.TrimSpace(e.Name)
	p := store.Product{
		Chain:      chainID,
		URL:        e.URL,
		SKU:        e.SKU,
		Name:       name,
		SearchName: strings.Join(match.Tokens(name), " "),
		Category:   e.Category,
		NameSource: "slug",
		Available:  true,
		CrawlState: "catalogado",
	}
	if e.Category == "" && name == "" {
		p.NameSource = "categoria"
	}
	if e.Category != "" && chainID == "dia" {
		// DÍA solo publica la categoría en el sitemap: el nombre real aparece
		// cuando la cola de precios visite la ficha.
		p.NameSource = "categoria"
		p.CrawlState = "ficha_pendiente"
	}
	if f := chain.FormatFromName(name); f != "" {
		p.Format = f
		if m, ok := match.ParseMeasure(f); ok {
			p.MeasureValue = m.Value
			p.MeasureUnit = m.Unit
		}
	}
	return p
}
