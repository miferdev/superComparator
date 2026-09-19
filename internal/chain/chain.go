// Package chain define el puerto común que implementan las cadenas
// (Mercadona, Ahorramas) y los tipos compartidos de producto.
package chain

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound indica que la ficha existe pero ya no está disponible.
var ErrNotFound = errors.New("producto no encontrado")

type SitemapEntry struct {
	URL     string
	Name    string
	SKU     string
	LastMod time.Time
}

type Product struct {
	Chain        string
	URL          string
	SKU          string
	Name         string
	Brand        string
	Format       string
	Category     string
	Price        float64
	UnitPrice    float64
	MeasurePrice float64
	MeasureUnit  string
	OldPrice     float64
	PromoText    string
	Available    bool
	FetchedAt    time.Time
}

func (p Product) HasPromo() bool {
	return p.OldPrice > p.Price && p.Price > 0
}

// Chain es el puerto que consume el núcleo. Los adaptadores no se conocen
// entre sí y solo pueden importar este paquete.
type Chain interface {
	ID() string
	Sitemap(ctx context.Context) ([]SitemapEntry, error)
	Fetch(ctx context.Context, url string) (Product, error)
}
