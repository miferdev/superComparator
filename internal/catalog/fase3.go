package catalog

import (
	"math"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/store"
)

// Fuente dice de dónde sale el precio que se enseña. Alcampo está detrás de un
// WAF y no da precios, así que sus fichas las precio a mano el usuario: sin esto
// no se podría distinguir un dato mío de uno de la tienda.
const (
	FuenteWeb     = "web"
	FuenteManual  = "manual"
	FuenteNinguno = "ninguno"
)

// PrecioManualDTO es un precio puesto a mano, para la web.
type PrecioManualDTO struct {
	Cadena       string    `json:"cadena"`
	URL          string    `json:"url"`
	Nombre       string    `json:"nombre"`
	Precio       float64   `json:"precio"`
	PrecioMedida float64   `json:"precioMedida"`
	Medida       string    `json:"medida"`
	Nota         string    `json:"nota"`
	Actualizado  time.Time `json:"actualizado"`
}

// SetPrecioManual guarda un precio puesto a mano. No toca el precio que publica
// la tienda: viven en tablas distintas a propósito, para que un dato mío no se
// confunda nunca con uno de la web.
func (c *Catalog) SetPrecioManual(chainID, productURL string, precio, precioMedida float64, medida, nota string) error {
	return c.store.SetPrecioManual(store.PrecioManual{
		Chain:        chainID,
		ProductURL:   productURL,
		Precio:       precio,
		PrecioMedida: precioMedida,
		Medida:       normalizarMedida(medida),
		Nota:         nota,
	})
}

// GetPrecioManual devuelve el precio manual de un producto, si lo hay.
func (c *Catalog) GetPrecioManual(chainID, productURL string) (PrecioManualDTO, bool, error) {
	m, ok, err := c.store.GetPrecioManual(chainID, productURL)
	if err != nil || !ok {
		return PrecioManualDTO{}, ok, err
	}
	dto := aPrecioManual(m)
	if p, ok, err := c.store.Product(chainID, productURL); err == nil && ok {
		dto.Nombre = p.Name
	}
	return dto, true, nil
}

// BorrarPrecioManual quita el precio manual y deja el de la tienda como estaba.
func (c *Catalog) BorrarPrecioManual(chainID, productURL string) error {
	return c.store.BorrarPrecioManual(chainID, productURL)
}

// PreciosManuales lista todos los precios puestos a mano, con el nombre del
// producto para que la web no tenga que pedirlos uno a uno.
func (c *Catalog) PreciosManuales() ([]PrecioManualDTO, error) {
	manuales, err := c.store.PreciosManuales()
	if err != nil {
		return nil, err
	}
	nombres := c.nombresDe(manuales)
	out := make([]PrecioManualDTO, 0, len(manuales))
	for _, m := range manuales {
		dto := aPrecioManual(m)
		dto.Nombre = nombres[m.Chain+"|"+m.ProductURL]
		out = append(out, dto)
	}
	return out, nil
}

// nombresDe busca el nombre de los productos de unos precios manuales. Va en una
// consulta con IN y no uno por precio: son unos cuantos, pero el patrón de miles
// de líneas de la lista lo notaría igual.
func (c *Catalog) nombresDe(manuales []store.PrecioManual) map[string]string {
	salida := map[string]string{}
	if len(manuales) == 0 {
		return salida
	}
	urls := make([]string, 0, len(manuales))
	vistos := map[string]bool{}
	for _, m := range manuales {
		if !vistos[m.ProductURL] {
			vistos[m.ProductURL] = true
			urls = append(urls, m.ProductURL)
		}
	}
	productos, err := c.store.ProductsByURL(urls)
	if err != nil {
		return salida
	}
	for _, p := range productos {
		salida[p.Chain+"|"+p.URL] = p.Name
	}
	return salida
}

func aPrecioManual(m store.PrecioManual) PrecioManualDTO {
	return PrecioManualDTO{
		Cadena:       m.Chain,
		URL:          m.ProductURL,
		Precio:       m.Precio,
		PrecioMedida: m.PrecioMedida,
		Medida:       m.Medida,
		Nota:         m.Nota,
		Actualizado:  m.Actualizado,
	}
}

// MiCompraDTO es «Mi compra» con sus totales.
type MiCompraDTO struct {
	Items             []ListaItemDTO   `json:"items"`
	SubtotalPorCadena []SubtotalCadena `json:"subtotalPorCadena"`
	Total             float64          `json:"total"`
	SinPrecio         int              `json:"sinPrecio"`
	SinPrecioDetalle  []string         `json:"sinPrecioDetalle"`
}

// ListaItemDTO es una línea de «Mi compra».
type ListaItemDTO struct {
	ID               int64     `json:"id"`
	Cadena           string    `json:"cadena"`
	NombreCadena     string    `json:"nombreCadena"`
	URL              string    `json:"url"`
	Nombre           string    `json:"nombre"`
	Formato          string    `json:"formato"`
	Cantidad         float64   `json:"cantidad"`
	Precio           float64   `json:"precio"`
	PrecioFuente     string    `json:"precioFuente"`
	PrecioMedida     float64   `json:"precioMedida"`
	Medida           string    `json:"medida"`
	PrecioSoloMedida bool      `json:"precioSoloMedida"`
	Subtotal         float64   `json:"subtotal"`
	Añadido          time.Time `json:"añadido"`
}

// SubtotalCadena es lo que se lleva una tienda.
type SubtotalCadena struct {
	Cadena   string  `json:"cadena"`
	Nombre   string  `json:"nombre"`
	Subtotal float64 `json:"subtotal"`
}

// MiCompra devuelve la lista con sus totales.
//
// El total solo suma precios de unidad: lo vendido al peso no tiene precio de
// unidad (su `price` está a 0 a propósito) y no se puede saber cuánto cuesta
// una bolsa de plátanos a partir del €/kg. Esas líneas van aparte, en SinPrecio,
// en vez de estimarlas.
func (c *Catalog) MiCompra() (MiCompraDTO, error) {
	l, err := c.store.ListaCompra()
	if err != nil {
		return MiCompraDTO{}, err
	}
	out := MiCompraDTO{
		Items:             make([]ListaItemDTO, 0, len(l.Items)),
		SubtotalPorCadena: []SubtotalCadena{},
		SinPrecioDetalle:  []string{},
	}
	for _, it := range l.Items {
		out.Items = append(out.Items, ListaItemDTO{
			ID:               it.ID,
			Cadena:           it.Chain,
			NombreCadena:     it.NombreCadena,
			URL:              it.ProductURL,
			Nombre:           it.Nombre,
			Formato:          it.Formato,
			Cantidad:         it.Cantidad,
			Precio:           céntimos(it.Precio),
			PrecioFuente:     fuenteDe(it.Fuente),
			PrecioMedida:     céntimos(it.PrecioMedida),
			Medida:           it.Medida,
			PrecioSoloMedida: it.PrecioSoloMedida,
			Subtotal:         céntimos(it.Subtotal),
			Añadido:          it.Añadido,
		})
		if it.Precio <= 0 {
			out.SinPrecioDetalle = append(out.SinPrecioDetalle, it.Nombre)
		}
	}
	// El store solo trae cadenas con algo sumable; en la web encaja más una lista
	// ordenada de mayor a menor.
	for cadena, sub := range l.SubtotalPorCadena {
		out.SubtotalPorCadena = append(out.SubtotalPorCadena, SubtotalCadena{
			Cadena:   cadena,
			Nombre:   l.SubtotalPorCadenaNombres[cadena],
			Subtotal: céntimos(sub),
		})
	}
	ordenaPorSubtotal(out.SubtotalPorCadena)
	out.Total = céntimos(l.Total)
	out.SinPrecio = l.SinPrecio
	return out, nil
}

// AñadirALaCompra mete un producto en «Mi compra».
func (c *Catalog) AñadirALaCompra(chainID, productURL string, cantidad float64) error {
	_, err := c.store.AddToList(chainID, productURL, cantidad)
	return err
}

// CambiarCantidad cambia cuántas unidades hay de un producto. Cantidad 0 o
// menor lo quita de la lista, que es lo que el usuario quiere decir.
func (c *Catalog) CambiarCantidad(chainID, productURL string, cantidad float64) error {
	if cantidad <= 0 {
		return c.store.RemoveFromList(chainID, productURL)
	}
	return c.store.SetListQuantity(chainID, productURL, cantidad)
}

// QuitarDeLaCompra saca un producto de «Mi compra».
func (c *Catalog) QuitarDeLaCompra(chainID, productURL string) error {
	return c.store.RemoveFromList(chainID, productURL)
}

// VaciarLaCompra deja la lista vacía.
func (c *Catalog) VaciarLaCompra() error {
	return c.store.ClearList()
}

func fuenteDe(f string) string {
	if f == "" {
		return FuenteNinguno
	}
	return f
}

// céntimos redondea a dos decimales. Los subtotales son sumas de flotantes y sin
// esto un total que se enseña como dinero sale como 5.6000000000000005.
func céntimos(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

func ordenaPorSubtotal(s []SubtotalCadena) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Subtotal > s[j-1].Subtotal; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// normalizarMedida deja la medida en minúsculas y sin espacios, para que "KG" y
// "kg" sean lo mismo al escribir un precio a mano.
func normalizarMedida(m string) string {
	return strings.ToLower(strings.TrimSpace(m))
}
