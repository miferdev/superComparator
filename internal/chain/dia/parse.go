// Package dia implementa el puerto chain contra la tienda online de DÍA
// (dia.es): sitemap plano de fichas y fichas server-rendered con JSON-LD.
//
// Su sitemap no incluye el nombre del producto, solo la ruta de categoría
// (/categoria/subcategoria/p/id), así que el adaptador agrupa las fichas por
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
		Category:  categoryFromURL(url),
	}

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var ld jsonldProduct
		if err := json.Unmarshal([]byte(s.Text()), &ld); err != nil {
			return true
		}
		if !isProduct(ld.Type) {
			return true
		}
		p.Name = strings.TrimSpace(ld.Name)
		p.Brand = strings.TrimSpace(ld.Brand.Name)
		if f, ok := chain.ParseJSONNumber(string(ld.Offers.Price)); ok {
			p.Price = f
		}
		if strings.Contains(strings.ToLower(ld.Offers.Availability), "outofstock") {
			p.Available = false
		}
		return false
	})
	if p.Name == "" || p.Price <= 0 {
		return chain.Product{}, errNoProduct
	}

	// El precio por unidad de DÍA viene en el HTML, con la unidad en mayúsculas.
	if m, ok := chain.ParseMeasurePrice(doc.Text()); ok {
		p.MeasurePrice = m.Value
		p.MeasureUnit = m.Unit
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

// pathSegments devuelve los segmentos de la ruta de una URL de DÍA.
func pathSegments(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return strings.Split(strings.Trim(u.Path, "/"), "/")
}
