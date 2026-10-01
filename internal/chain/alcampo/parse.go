package alcampo

import (
	"encoding/json"
	"errors"
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

// ParseProduct extrae el producto de una ficha de Alcampo. El precio y la
// disponibilidad vienen en el JSON-LD; el precio por unidad se busca en el
// texto de la página, que lo muestra junto a la medida.
func ParseProduct(html, url string) (chain.Product, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return chain.Product{}, err
	}

	p := chain.Product{
		Chain:     "alcampo",
		URL:       url,
		Available: true,
		FetchedAt: time.Now(),
		SKU:       entryFromURL(url, "").SKU,
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
		if ld.SKU != "" {
			p.SKU = ld.SKU
		}
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

	if m, ok := chain.ParseMeasurePrice(doc.Text()); ok {
		p.MeasurePrice = m.Value
		p.MeasureUnit = m.Unit
	}
	p.UnitPrice = p.Price
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
