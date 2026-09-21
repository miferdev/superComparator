package core

import "github.com/miferdev/superComparator/internal/chain"

// Criterios con los que se decide la opción más barata de un producto.
const (
	criterionPerKg = "€/kg"
	criterionPerL  = "€/l"
	criterionTotal = "total"
)

// comparable es lo mínimo que necesita la regla de comparación para decidir
// qué opción es más barata.
type comparable struct {
	price        float64
	measurePrice float64
	measureUnit  string
	available    bool
	stale        bool
}

func comparableOfProduct(p chain.Product) comparable {
	return comparable{
		price:        p.Price,
		measurePrice: p.MeasurePrice,
		measureUnit:  p.MeasureUnit,
		available:    p.Available,
	}
}

func comparableOfOption(o ChainOption) comparable {
	return comparable{
		price:        o.Price,
		measurePrice: o.MeasurePrice,
		measureUnit:  o.MeasureUnit,
		available:    o.Available,
		stale:        o.Stale,
	}
}

// cheapestIndex elige la opción más barata con una regla explícita y devuelve
// también el criterio usado. Nunca compara €/medida de dimensiones distintas
// (kg frente a l) ni €/medida con un precio total: si no hay al menos dos
// opciones cotizando en la misma unidad canónica, decide por precio total.
func cheapestIndex(opts []comparable) (int, string) {
	valid := make([]int, 0, len(opts))
	for i, o := range opts {
		if o.price > 0 && o.available && !o.stale {
			valid = append(valid, i)
		}
	}
	if len(valid) == 0 {
		return -1, ""
	}
	if idx, ok := cheapestByMeasure(opts, valid, "kg"); ok {
		return idx, criterionPerKg
	}
	if idx, ok := cheapestByMeasure(opts, valid, "l"); ok {
		return idx, criterionPerL
	}
	best := valid[0]
	for _, i := range valid[1:] {
		if opts[i].price < opts[best].price {
			best = i
		}
	}
	return best, criterionTotal
}

// cheapestByMeasure solo decide por €/medida cuando al menos dos opciones
// cotizan en la misma unidad canónica; con una sola no son comparables.
func cheapestByMeasure(opts []comparable, valid []int, unit string) (int, bool) {
	best, count := -1, 0
	for _, i := range valid {
		if opts[i].measureUnit != unit || opts[i].measurePrice <= 0 {
			continue
		}
		count++
		if best == -1 || opts[i].measurePrice < opts[best].measurePrice {
			best = i
		}
	}
	if count < 2 {
		return -1, false
	}
	return best, true
}

// CheapestIndex expone la regla de comparación (índice y criterio) para que la
// interfaz pueda marcar la alternativa más barata sin duplicar la lógica.
func CheapestIndex(opts []ChainOption) (int, string) {
	comps := make([]comparable, len(opts))
	for i, o := range opts {
		comps[i] = comparableOfOption(o)
	}
	return cheapestIndex(comps)
}

// runnerUpPrice devuelve el precio total de la opción válida más barata
// distinta de la elegida, para estimar el ahorro del producto.
func runnerUpPrice(opts []comparable, chosen int) (float64, bool) {
	best, found := 0.0, false
	for i, o := range opts {
		if i == chosen || o.price <= 0 || !o.available || o.stale {
			continue
		}
		if !found || o.price < best {
			best, found = o.price, true
		}
	}
	return best, found
}
