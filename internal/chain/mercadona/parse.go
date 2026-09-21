package mercadona

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/miferdev/superComparator/internal/chain"
)

var errNoProduct = errors.New("ficha sin datos de producto")

var productRe = regexp.MustCompile(`/product/(\d+)/`)

// ParseProduct extrae el producto del DOM renderizado de una ficha de Mercadona.
func ParseProduct(html, url string) (chain.Product, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return chain.Product{}, err
	}

	detail := doc.Find(".private-product-detail").First()
	if detail.Length() == 0 {
		return chain.Product{}, errNoProduct
	}

	p := chain.Product{
		Chain:     "mercadona",
		URL:       url,
		FetchedAt: time.Now(),
	}
	if m := productRe.FindStringSubmatch(url); m != nil {
		p.SKU = m[1]
	}

	p.Name = strings.TrimSpace(detail.Find("h1.private-product-detail__description").First().Text())
	if p.Name == "" {
		return chain.Product{}, errNoProduct
	}

	var spans []string
	detail.Find(".product-format__size").First().Find(`span[aria-hidden="true"]`).Each(func(_ int, s *goquery.Selection) {
		spans = append(spans, s.Text())
	})
	formatText := strings.TrimSpace(strings.Join(spans, ""))
	if before, after, ok := strings.Cut(formatText, "|"); ok {
		p.Format = strings.TrimSpace(before)
		if m, ok := chain.ParseMeasurePrice(after); ok {
			p.MeasurePrice = m.Value
			p.MeasureUnit = m.Unit
		}
	} else {
		p.Format = chain.FormatFromName(p.Name)
	}

	priceText := detail.Find("p.product-price__unit-price").First().Text()
	if f, ok := chain.ParsePrice(priceText); ok {
		p.Price = f
		p.UnitPrice = f
		p.Available = true
	}
	extra := strings.ToLower(detail.Find("p.product-price__extra-price").First().Text())
	if p.MeasureUnit == "" && strings.Contains(extra, "/ud") {
		p.MeasureUnit = "ud"
	}

	if old := detail.Find("[class*='previous'], s, del").First().Text(); old != "" {
		if f, ok := chain.ParsePrice(old); ok {
			p.OldPrice = f
		}
	}
	if promo := detail.Find("[class*='promo'], [class*='offer']").First().Text(); promo != "" {
		p.PromoText = strings.Join(strings.Fields(promo), " ")
	}
	return p, nil
}
