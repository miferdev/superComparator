// Package dia implementa el puerto chain contra la tienda online de DÍA
// (dia.es): sitemap plano de fichas y fichas server-rendered con JSON-LD.
//
// Su sitemap no incluye el nombre del producto, solo la ruta de categoría
// (/categoria/subcategoria/p/id), de la que sale la categoría legible de cada
// entrada. Como no trae el nombre, el adaptador agrupa las fichas por
// categoría, elige las que encajan con la consulta y las reparte de forma
// espaciada. El nombre real llega de la ficha, y si la coincidencia final no
// supera el umbral del núcleo la coincidencia se marca para revisión manual.
package dia

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/miferdev/superComparator/internal/chain"
)

var errNoProduct = errors.New("ficha sin datos de producto")

type jsonldProduct struct {
	Type  string `json:"@type"`
	Name  string `json:"name"`
	SKU   string `json:"sku"`
	Brand struct {
		Name string `json:"name"`
	} `json:"brand"`
	Offers struct {
		Price         chain.JSONText `json:"price"`
		PriceCurrency string         `json:"priceCurrency"`
		Availability  string         `json:"availability"`
	} `json:"offers"`
}

// ParseProduct extrae el producto de una ficha de DÍA.
func ParseProduct(html, url string) (chain.Product, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return chain.Product{}, err
	}

	p := chain.Product{
		Chain:     "dia",
		URL:       url,
		Available: true,
		FetchedAt: time.Now(),
		SKU:       productID(url),
		Category:  categoryNameFromURL(url),
	}

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var ld jsonldProduct
		if err := json.Unmarshal([]byte(s.Text()), &ld); err != nil {
			return true
		}
		if !isProduct(ld.Type) {
			return true
		}
		// El JSON-LD es texto plano para el parser de HTML, así que sus entidades
		// llegan sin desescapar ("Hellmann&#039;s") y se guardan tal cual: hay que
		// desescaparlas aquí, y solo aquí, porque el resto de la ficha sale del
		// DOM y viene ya desescapado.
		p.Name = chain.UnescapeText(strings.TrimSpace(ld.Name))
		p.Brand = chain.UnescapeText(strings.TrimSpace(ld.Brand.Name))
		if f, ok := chain.ParseJSONNumber(string(ld.Offers.Price)); ok {
			p.Price = f
		}
		if strings.Contains(strings.ToLower(ld.Offers.Availability), "outofstock") {
			p.Available = false
		}
		return false
	})
	// El precio por unidad de DÍA viene en un elemento aparte del buy-box. Si el
	// precio publicado lleva la unidad encima (producto vendido al peso), lo
	// único publicado es ese: Price se deja a 0 en vez de servir el €/kg como si
	// fuera el de una bolsa.
	activo := chain.NormalizeSpaces(doc.Find(".buy-box__active-price").First().Text())
	if m, ok := chain.ParseMeasurePrice(activo); ok {
		p.Price = 0
		p.PriceIsPerMeasure = true
		p.MeasurePrice = m.Value
		p.MeasureUnit = m.Unit
	} else if m, ok := chain.ParseMeasurePrice(doc.Find(".buy-box__price-per-unit").First().Text()); ok {
		p.MeasurePrice = m.Value
		p.MeasureUnit = m.Unit
	}
	if p.Name == "" || (p.Price <= 0 && p.MeasurePrice <= 0) {
		return chain.Product{}, errNoProduct
	}
	p.UnitPrice = p.Price

	if old := doc.Find(".buy-box__previous-price, .product-price__previous-price").First().Text(); old != "" {
		if f, ok := chain.ParsePrice(old); ok {
			p.OldPrice = f
		}
	}
	if tag := doc.Find(".buy-box__promo-tag, .product-price__discount").First().Text(); tag != "" {
		p.PromoText = strings.Join(strings.Fields(tag), " ")
	}

	p.Format = chain.FormatFromName(p.Name)
	return p, nil
}

// isProduct acepta el tipo JSON-LD tanto simple como en lista.
func isProduct(t string) bool {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "[") {
		var types []string
		if err := json.Unmarshal([]byte(t), &types); err != nil {
			return false
		}
		for _, one := range types {
			if strings.EqualFold(one, "Product") {
				return true
			}
		}
		return false
	}
	return strings.EqualFold(t, "Product")
}

// productID saca el identificador de /categoria/subcategoria/p/12345.
func productID(rawURL string) string {
	segments := pathSegments(rawURL)
	if len(segments) == 0 {
		return ""
	}
	return segments[len(segments)-1]
}

// categoryFromURL devuelve la ruta "categoria/subcategoria" de la ficha.
func categoryFromURL(rawURL string) string {
	var cat []string
	for _, s := range pathSegments(rawURL) {
		if s == "p" {
			break
		}
		cat = append(cat, s)
	}
	return strings.Join(cat, "/")
}

// categoryNameFromURL devuelve la categoría de la ficha como nombre legible,
// con los niveles separados por " / ": la ruta
// /huevos-leche-y-mantequilla/leche/p/16065 se lee "Huevos leche y
// mantequilla / Leche". Cada nivel se humaniza por separado, porque
// HumanizeSlug convierte también las barras en espacios y perdería la
// jerarquía.
func categoryNameFromURL(rawURL string) string {
	segments := strings.Split(categoryFromURL(rawURL), "/")
	names := make([]string, 0, len(segments))
	for _, s := range segments {
		names = append(names, capitalize(chain.HumanizeSlug(s)))
	}
	return chain.CategoryPath(names...)
}

// capitalize sube la inicial de un nombre de categoría sin tocar el resto.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// pathSegments devuelve los segmentos de la ruta de una URL de DÍA.
func pathSegments(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return strings.Split(strings.Trim(u.Path, "/"), "/")
}
