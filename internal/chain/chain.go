// Package chain define el puerto común que implementan las cadenas
// (Mercadona, Ahorramas, DÍA y Alcampo) y los tipos compartidos de producto.
package chain

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// UserAgent identifica al proyecto de forma honesta en las peticiones HTTP
// simples, indicando dónde está el código.
const UserAgent = "supercomparator/0.1 (+https://github.com/miferdev/superComparator)"

// ErrNotFound indica que la ficha existe pero ya no está disponible.
var ErrNotFound = errors.New("producto no encontrado")

// NewHTTPClient devuelve el cliente que usan los adaptadores que hablan HTTP
// directamente, con identificación honesta y cabecera de idioma.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

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

// Alternative es un candidato descartado al resolver un producto: sirve para
// proponer opciones cuando la coincidencia no es concluyente.
type Alternative struct {
	URL          string
	Name         string
	Score        float64
	Price        float64
	MeasurePrice float64
	MeasureUnit  string
}

// Lookup es una extensión opcional del puerto para las cadenas cuyo catálogo no
// se puede resolver con el sitemap: sin ella, el núcleo busca en Sitemap.
type Lookup interface {
	Lookup(ctx context.Context, query string) ([]SitemapEntry, error)
}

// Chain es el puerto que consume el núcleo. Los adaptadores no se conocen
// entre sí y solo pueden importar este paquete.
type Chain interface {
	ID() string
	Sitemap(ctx context.Context) ([]SitemapEntry, error)
	Fetch(ctx context.Context, url string) (Product, error)
}
