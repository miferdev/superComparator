package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/store"
)

// PricesJob es el trabajo de segundo plano que rellena los precios del
// catálogo. El bucle (en store) es quien respeta el ritmo de cada tienda y
// decide qué ficha toca; aquí solo se le da quién descarga las fichas.
type PricesJob struct {
	loop   *store.PriceLoop
	cat    *Catalog
	chains []chain.Chain
	log    *slog.Logger
}

// NewPricesJob monta el trabajo de precios para un conjunto de tiendas. El ritmo
// y si los precios están activos no se le pasan: ya están en la tabla chains, que
// es donde el usuario puede ajustarlos sin recompilar.
func NewPricesJob(cat *Catalog, chains []chain.Chain) *PricesJob {
	// El bucle solo puede trabajar en tiendas para las que hay adaptador, y su
	// ritmo sale de la base: se arma la lista con las cadenas seleccionadas.
	sel, err := cat.cadenasDePrecio(chains)
	if err != nil {
		cat.log.Warn("sin cola de precios: no se pudieron leer las cadenas", "err", err)
	}
	loop := store.NewPriceLoop(cat.store, NewFetcher(chains), sel).WithLogger(cat.log)
	job := &PricesJob{
		loop:   loop,
		cat:    cat,
		chains: chains,
		log:    cat.log,
	}
	// El nombre de la ficha se normaliza aquí, que es donde vive match: store no
	// sabe tokenizar y no debe saberlo.
	loop.WithOnFicha(job.ActualizaFicha)
	return job
}

// Run rellena precios hasta que no queda nada pendiente o se cancela.
func (j *PricesJob) Run(ctx context.Context) (store.PriceJobResult, error) {
	return j.loop.RunUntilEmpty(ctx)
}

// RunLimited hace una sola pasada de limit fichas: lo que usa `crawl precios` y
// lo que permite probar contra la web real sin esperar horas.
func (j *PricesJob) RunLimited(ctx context.Context, limit int) (store.PriceJobResult, error) {
	return j.loop.Run(ctx, limit)
}

// ActualizaFicha guarda el nombre y la categoría definitivos de un producto a
// partir de la ficha recién descargada. Sobre todo sirve para DÍÁ, cuyo sitemap
// solo trae la categoría: sin esto sus productos se llaman «leche» y no
// aparecen al buscar «leche entera».
func (j *PricesJob) ActualizaFicha(productID int64, p chain.Product) {
	if p.Name == "" && p.Category == "" {
		return
	}
	if err := j.cat.SetFichaDatos(productID, p.Name, p.Category); err != nil {
		j.log.Debug("no se pudo actualizar la ficha", "producto", productID, "err", err)
	}
}

// QueueStats resume la cola por cadena, para la pantalla de estado.
func (j *PricesJob) QueueStats(chainID string) (map[string]int, error) {
	return j.loop.Stats(chainID)
}

// cadenasDePrecio cruza los adaptadores con lo que hay en la base: de aquí sale
// el ritmo, la concurrencia y si los precios de esa tienda están apagados (Alcampo
// lo tiene, por su WAF).
func (c *Catalog) cadenasDePrecio(chains []chain.Chain) ([]store.Chain, error) {
	out := make([]store.Chain, 0, len(chains))
	for _, ch := range chains {
		guardada, ok, err := c.store.Chain(ch.ID())
		if err != nil {
			return nil, err
		}
		if !ok {
			// Sin fila en chains no hay ritmo con el que trabajar: mejor no
			// tocarla que golpearla sin pausa.
			c.log.Warn("cadena sin configuración en la base, se deja aparte", "cadena", ch.ID())
			continue
		}
		out = append(out, guardada)
	}
	return out, nil
}

// NewFetcher agrupa los adaptadores para poder usarlos como store.PriceFetcher.
func NewFetcher(chains []chain.Chain) store.PriceFetcher {
	return fetcher{chains: indexChains(chains)}
}

type fetcher struct {
	chains map[string]chain.Chain
}

func indexChains(chains []chain.Chain) map[string]chain.Chain {
	m := make(map[string]chain.Chain, len(chains))
	for _, ch := range chains {
		m[ch.ID()] = ch
	}
	return m
}

func (f fetcher) Fetch(ctx context.Context, chainID, url string) (chain.Product, error) {
	ch, ok := f.chains[chainID]
	if !ok {
		return chain.Product{}, fmt.Errorf("cadena desconocida: %s", chainID)
	}
	return ch.Fetch(ctx, url)
}

// PriceEvent es un aviso del trabajo de precios, para el SSE de la web.
type PriceEvent struct {
	Tipo     string    `json:"tipo"`
	Cadena   string    `json:"cadena,omitempty"`
	Producto string    `json:"producto,omitempty"`
	Precio   float64   `json:"precio,omitempty"`
	Error    string    `json:"error,omitempty"`
	Estado   string    `json:"estado,omitempty"`
	Hechos   int       `json:"hechos"`
	Total    int       `json:"total"`
	Cuándo   time.Time `json:"cuando"`
}
