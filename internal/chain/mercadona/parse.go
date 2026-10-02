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
	p.Category = categoryFromBreadcrumb(detail)

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

	priceSel := detail.Find("p.product-price__unit-price").First()
	priceText := strings.Join(strings.Fields(priceSel.Text()), " ")
	if f, ok := chain.ParsePrice(priceText); ok {
		p.Available = true
		// Los productos que Mercadona vende al peso (plátanos, carne…) publican
		// el precio por medida en el propio precio ("1,65 €/kg"). Lo único
		// publicado es ese, así que Price se deja a 0: poner 1,65 ahí haría que
		// la web enseñase el precio del kilo como si fuera el de una bolsa.
		if m, ok := chain.ParseMeasurePrice(priceText); ok {
			p.PriceIsPerMeasure = true
			p.MeasurePrice = m.Value
			p.MeasureUnit = m.Unit
			if p.Format == "" {
				p.Format = "1 " + m.Unit
			}
		} else {
			p.Price = f
			p.UnitPrice = f
		}
	}
	extra := strings.ToLower(detail.Find("p.product-price__extra-price").First().Text())
	if p.MeasureUnit == "" && strings.Contains(extra, "/ud") {
		p.MeasureUnit = "ud"
	}

	// Solo cuenta como precio anterior el que Mercadona marca como tal: antes
	// cualquier <del> o <s> de la ficha servía, y de ahí salían ofertas falsas.
	if old := detail.Find("[class*='previous-price'], [class*='old-price'], [class*='price-old'], [class*='previous']").First().Text(); old != "" {
		if f, ok := chain.ParsePrice(old); ok {
			p.OldPrice = f
		}
	}
	if promo := detail.Find("[class*='promo'], [class*='offer']").First().Text(); promo != "" {
		p.PromoText = strings.Join(strings.Fields(promo), " ")
	}
	return p, nil
}

// categoryFromBreadcrumb saca la categoría de la miga de pan de la ficha: el
// nivel superior y el fino llegan en dos span ("Cacao, café e infusiones >" y
// "Cacao soluble y chocolate a la taza") y se unen con " / ", el formato que ya
// usa DÍA. La miga es la única parte de la ficha que describe la sección: el
// nombre del producto no la lleva.
func categoryFromBreadcrumb(detail *goquery.Selection) string {
	var niveles []string
	detail.Find(".private-product-detail__header-breadcrumb a").Each(func(_ int, a *goquery.Selection) {
		a.Find("span").Each(func(_ int, s *goquery.Selection) {
			niveles = append(niveles, breadcrumbLevel(s.Text()))
		})
	})
	return chain.CategoryPath(niveles...)
}

// breadcrumbLevel limpia un nivel de la miga: el superior llega con la flecha
// de separación al final.
func breadcrumbLevel(text string) string {
	text = strings.TrimSpace(chain.NormalizeSpaces(text))
	if antes, _, ok := strings.Cut(text, ">"); ok {
		text = strings.TrimSpace(antes)
	}
	return text
}
