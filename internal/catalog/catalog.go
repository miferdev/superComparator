package catalog

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/store"
)

// Product es la forma de un producto tal como la ve la web: con la etiqueta de
// la tienda y la fecha en que se comprobó el precio.
type Product struct {
	ID           int64     `json:"id"`
	Chain        string    `json:"cadena"`
	NombreCadena string    `json:"nombreCadena"`
	URL          string    `json:"url"`
	Nombre       string    `json:"nombre"`
	SKU          string    `json:"sku"`
	Formato      string    `json:"formato"`
	MedidaValor  float64   `json:"medidaValor"`
	MedidaUnidad string    `json:"medidaUnidad"`
	Categoria    string    `json:"categoria"`
	Precio       float64   `json:"precio"`
	PrecioBase   string    `json:"precioBase"`
	PrecioMedida float64   `json:"precioMedida"`
	Disponible   bool      `json:"disponible"`
	PrecioEn     time.Time `json:"precioComprobado"`
	NombreFuente string    `json:"nombreFuente"`
	TienePrecio  bool      `json:"tienePrecio"`
	// PrecioSoloMedida es true cuando la tienda solo publica €/kg o €/l (plátanos
	// a peso, por ejemplo). Entonces no hay precio de unidad y no se puede
	// sumar al total de la compra, pero el dato existe y hay que enseñarlo.
	PrecioSoloMedida bool `json:"precioSoloMedida"`
	PrecioViejo      bool `json:"precioViejo"`
	FrescuraHoras    int  `json:"frescuraHoras"`
}

// Chain es una cadena tal como la ve la web: su configuración y cuánto catálogo
// tiene ya guardado.
type Chain struct {
	ID             string  `json:"id"`
	Nombre         string  `json:"nombre"`
	PreciosActivos bool    `json:"preciosActivos"`
	PausaSegundos  float64 `json:"pausaSegundos"`
	PrecioMaxHoras int     `json:"precioMaxHoras"`
	Total          int     `json:"total"`
	ConPrecio      int     `json:"conPrecio"`
	SinPrecio      int     `json:"sinPrecio"`
	Fase           string  `json:"fase"`
	Pausada        bool    `json:"pausada"`
}

// Catalog responde a lo que pide la web: buscar, ver un producto, saber por
// dónde va cada cadena.
type Catalog struct {
	store    *store.Store
	log      *slog.Logger
	cadenas  map[string]store.Chain
	cargadas bool
}

func New(st *store.Store, log *slog.Logger) *Catalog {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Catalog{store: st, log: log, cadenas: make(map[string]store.Chain)}
}

// Seed registra la configuración de las cadenas disponibles.
func (c *Catalog) Seed(chains []store.Chain) error {
	for _, ch := range chains {
		if err := c.store.SeedChains([]store.Chain{ch}); err != nil {
			return err
		}
	}
	return nil
}

// cadenasPorID carga la configuración de las cadenas una vez y la reutiliza, para
// poder poner el nombre comercial (Alcampo) en los productos sin exigir que
// nadie haya llamado antes a Chains.
func (c *Catalog) cadenasPorID(ctx context.Context) map[string]store.Chain {
	if c.cargadas {
		return c.cadenas
	}
	rows, err := c.store.Chains()
	if err != nil {
		c.log.Error("no se pudieron leer las cadenas", "err", err)
		return c.cadenas
	}
	for _, ch := range rows {
		c.cadenas[ch.ID] = ch
	}
	c.cargadas = true
	return c.cadenas
}

// Chains devuelve todas las cadenas con su recuento de catálogo.
func (c *Catalog) Chains(ctx context.Context) ([]Chain, error) {
	rows, err := c.store.Chains()
	if err != nil {
		return nil, err
	}
	counts, err := c.store.CatalogCounts()
	if err != nil {
		return nil, err
	}
	byChain := make(map[string]store.CatalogCount, len(counts))
	for _, n := range counts {
		byChain[n.Chain] = n
	}
	out := make([]Chain, 0, len(rows))
	for _, ch := range rows {
		c.cadenas[ch.ID] = ch
		cad := Chain{
			ID:             ch.ID,
			Nombre:         ch.Nombre,
			PreciosActivos: ch.PreciosActivos,
			PausaSegundos:  ch.PausaSegundos,
			PrecioMaxHoras: ch.PrecioMaxHoras,
		}
		if n, ok := byChain[ch.ID]; ok {
			cad.Total, cad.ConPrecio, cad.SinPrecio = n.Total, n.ConPrecio, n.SinPrecio
		}
		if st, err := c.store.CrawlState(ch.ID); err == nil {
			cad.Fase, cad.Pausada = st.Phase, st.Paused
		}
		out = append(out, cad)
	}
	return out, nil
}

// Search busca productos en el catálogo.
func (c *Catalog) Search(ctx context.Context, q store.SearchQuery) ([]Product, int, bool, error) {
	res, err := c.store.Search(q)
	if err != nil {
		return nil, 0, false, err
	}
	cadenas := c.cadenasPorID(ctx)
	maxHoras := 24
	if ch, ok := cadenas[q.Chain]; ok && ch.PrecioMaxHoras > 0 {
		maxHoras = ch.PrecioMaxHoras
	}
	out := make([]Product, 0, len(res.Products))
	for _, p := range res.Products {
		out = append(out, toProduct(p, cadenas, maxHoras))
	}
	return out, res.Total, res.HayMas, nil
}

// Product devuelve un producto por cadena y URL.
func (c *Catalog) Product(ctx context.Context, chainID, url string) (Product, bool, error) {
	p, ok, err := c.store.Product(chainID, url)
	if err != nil || !ok {
		return Product{}, ok, err
	}
	cadenas := c.cadenasPorID(ctx)
	maxHoras := 24
	if ch, known := cadenas[chainID]; known && ch.PrecioMaxHoras > 0 {
		maxHoras = ch.PrecioMaxHoras
	}
	return toProduct(p, cadenas, maxHoras), true, nil
}

func toProduct(p store.Product, cadenas map[string]store.Chain, maxHoras int) Product {
	out := Product{
		ID:               p.ID,
		Chain:            p.Chain,
		NombreCadena:     p.Chain,
		URL:              p.URL,
		Nombre:           p.Name,
		SKU:              p.SKU,
		Formato:          p.Format,
		MedidaValor:      p.MeasureValue,
		MedidaUnidad:     p.MeasureUnit,
		Categoria:        p.Category,
		Precio:           p.Price,
		PrecioBase:       p.PriceBasis,
		PrecioMedida:     p.MeasurePrice,
		Disponible:       p.Available,
		PrecioEn:         p.PriceFetchedAt,
		NombreFuente:     p.NameSource,
		TienePrecio:      p.Price > 0,
		PrecioSoloMedida: p.PriceBasis != "" && p.PriceBasis != "unidad" && p.Price <= 0,
	}
	if ch, ok := cadenas[p.Chain]; ok && ch.Nombre != "" {
		out.NombreCadena = ch.Nombre
	}
	if out.PrecioEn.IsZero() {
		return out
	}
	horas := int(time.Since(out.PrecioEn).Hours())
	out.FrescuraHoras = horas
	out.PrecioViejo = horas > maxHoras
	return out
}

// NormalizeSearchQuery deja los parámetros de búsqueda en su forma final,
// con valores por defecto razonables.
func NormalizeSearchQuery(q store.SearchQuery) store.SearchQuery {
	q.Text = strings.TrimSpace(q.Text)
	q.Chain = strings.TrimSpace(q.Chain)
	if q.Orden == "" {
		q.Orden = "relevancia"
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	return q
}
