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

type Comparison struct {
	Items         []ItemComparison
	Chains        []string
	Totals        map[string]float64
	MixedTotal    float64
	CheapestChain string
	MaxSaving     float64
	GeneratedAt   time.Time
	Review        []ReviewItem
	Changes       []store.Change
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
			opt, ok := optionByChain(item, chainID)
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

// optionByChain devuelve la opción de la cadena, exista o no con precio.
func optionByChain(item ItemComparison, chainID string) (ChainOption, bool) {
	for _, opt := range item.Options {
		if opt.Chain == chainID {
			return opt, true
		}
	}
	return ChainOption{}, false
}
