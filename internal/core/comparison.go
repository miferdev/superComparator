package core

import (
	"context"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/store"
)

// ChainOption es el resultado de un producto en una cadena concreta.
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
	Score        float64
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

// ReviewItem es un producto cuya coincidencia quedó por debajo del umbral
// automático: el informe debe pedir una revisión manual.
type ReviewItem struct {
	Name         string
	Chain        string
	Score        float64
	Alternatives []chain.Alternative
}

// ChainCoverage resume cuánto de la lista cubre una cadena. Una cadena que no
// tiene todos los productos no sirve para hacer la compra entera, así que su
// total no es comparable con el de las demás y no puede ganar el título de más
// barata.
type ChainCoverage struct {
	Chain    string
	Found    int
	Total    int
	Missing  []string
	Complete bool
}

type Comparison struct {
	Items  []ItemComparison
	Chains []string
	// Coverage dice, por cadena, cuántos productos de la lista tiene.
	Coverage []ChainCoverage
	// Totals es la suma de los productos encontrados en cada cadena: si la
	// cadena no cubre la lista, es un total parcial.
	Totals map[string]float64
	// MixedTotal es la compra compuesta por el producto más barato en cada
	// cadena. MixedStores dice en cuántas tiendas habría que entrar.
	MixedTotal    float64
	MixedStores   int
	Missing       []string
	CheapestChain string
	MaxSaving     float64
	GeneratedAt   time.Time
	Review        []ReviewItem
	Changes       []store.Change
}

// CoverageOf devuelve la cobertura de una cadena concreta: cuántos productos
// de la lista tiene y cuáles le faltan.
func (c *Comparison) CoverageOf(chainID string) ChainCoverage {
	for _, cov := range c.Coverage {
		if cov.Chain == chainID {
			return cov
		}
	}
	return ChainCoverage{Chain: chainID}
}

// MixedComplete indica si la compra mixta cubre la lista entera.
func (c *Comparison) MixedComplete() bool {
	return len(c.Missing) == 0 && len(c.Items) > 0
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
			Score:        row.Score,
			FetchedAt:    row.FetchedAt,
			Stale:        c.priceIsStale(row.FetchedAt),
		})
	}
	if err := c.addUnresolved(&cmp); err != nil {
		return Comparison{}, err
	}
	tiendas := make(map[string]bool, len(c.order))
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
			tiendas[ic.Cheapest] = true
			if next, ok := runnerUpPrice(comps, k); ok {
				ic.Save = (next - ic.Options[k].Price) * float64(ic.Quantity)
			}
		} else {
			// Ninguna cadena lo tiene con precio: el producto no se puede
			// comprar, y la compra mixta no cubre la lista.
			cmp.Missing = append(cmp.Missing, ic.Name)
		}
	}
	cmp.MixedStores = len(tiendas)
	cmp.FillCoverage()

	// Solo es «la más barata» la cadena que tiene la lista completa: comparar
	// un total parcial daría por ganadora a una cadena donde no podrías
	// comprar todo.
	best := ""
	for _, cov := range cmp.Coverage {
		if !cov.Complete {
			continue
		}
		if best == "" || cmp.Totals[cov.Chain] < cmp.Totals[best] {
			best = cov.Chain
		}
	}
	cmp.CheapestChain = best
	if best != "" {
		for _, cov := range cmp.Coverage {
			if cov.Complete && cov.Chain != best {
				if saving := cmp.Totals[cov.Chain] - cmp.Totals[best]; saving > cmp.MaxSaving {
					cmp.MaxSaving = saving
				}
			}
		}
	}
	if err := c.fillReview(&cmp); err != nil {
		return Comparison{}, err
	}
	if changes, err := c.store.Changes(20); err == nil {
		cmp.Changes = changes
	}
	return cmp, nil
}

// fillReview recoge lo que hay que revisar a mano: los productos que una cadena
// no ha podido resolver con confianza y los que sí, pero por debajo del umbral.
func (c *Core) fillReview(cmp *Comparison) error {
	alts, err := c.store.Alternatives()
	if err != nil {
		return err
	}
	for _, item := range cmp.Items {
		for _, chainID := range cmp.Chains {
			candidatos := alts[item.Name][chainID]
			opt, ok := pricedOption(item, chainID)
			switch {
			case ok && opt.Score < match.AutoThreshold:
				review := ReviewItem{Name: item.Name, Chain: chainID, Score: opt.Score}
				for _, alt := range candidatos {
					if alt.URL != opt.URL {
						review.Alternatives = append(review.Alternatives, alt)
					}
				}
				cmp.Review = append(cmp.Review, review)
			case !ok && len(candidatos) > 0:
				// La cadena no resolvió el producto: sus candidatos son lo más
				// parecido que ha encontrado, y la decisión es del usuario.
				cmp.Review = append(cmp.Review, ReviewItem{
					Name:         item.Name,
					Chain:        chainID,
					Score:        candidatos[0].Score,
					Alternatives: candidatos,
				})
			}
		}
	}
	return nil
}

// FillCoverage cuenta los productos de la lista que tiene cada cadena. Una
// cadena con productos de menos se considera incompleta y su total no sirve
// para hacer la compra entera.
func (c *Comparison) FillCoverage() {
	for _, chainID := range c.Chains {
		cov := ChainCoverage{Chain: chainID, Total: len(c.Items)}
		for _, item := range c.Items {
			if _, ok := pricedOption(item, chainID); ok {
				cov.Found++
				continue
			}
			cov.Missing = append(cov.Missing, item.Name)
		}
		cov.Complete = cov.Total > 0 && cov.Found == cov.Total
		c.Coverage = append(c.Coverage, cov)
	}
}

// pricedOption devuelve la opción con precio de una cadena.
func pricedOption(item ItemComparison, chainID string) (ChainOption, bool) {
	for _, opt := range item.Options {
		if opt.Chain == chainID && opt.Price > 0 {
			return opt, true
		}
	}
	return ChainOption{}, false
}

// addUnresolved añade a la comparativa los productos de la lista que no tienen
// ningún match, para que el informe los muestre como no encontrados en vez de
// hacerlos desaparecer.
func (c *Core) addUnresolved(cmp *Comparison) error {
	items, err := c.store.Items()
	if err != nil {
		return err
	}
	vistos := make(map[string]bool, len(cmp.Items))
	for _, item := range cmp.Items {
		vistos[item.Name] = true
	}
	for _, it := range items {
		if vistos[it.Name] {
			continue
		}
		cmp.Items = append(cmp.Items, ItemComparison{Name: it.Name, Quantity: it.Quantity})
	}
	return nil
}
