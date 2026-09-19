package ahorramas

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/miferdev/superComparator/internal/chain"
)

var errNoProduct = errors.New("ficha sin datos de producto")

var (
	measureRe = regexp.MustCompile(`(?i)([0-9]+,[0-9]+)&euro;/(KILO|KG|LITRO|L|ML)\b`)
	netWeight = regexp.MustCompile(`(?i)(?:Peso Neto escurrido|Contenido neto)\s*:\s*([0-9.,]+\s*[A-Za-z]+)`)
	promoRe   = regexp.MustCompile(`Bajada de precio a [^()]{0,60}\([^)]*\)`)
)

type jsonldProduct struct {
	Type  string `json:"@type"`
	Name  string `json:"name"`
	SKU   string `json:"sku"`
	Brand struct {
		Name string `json:"name"`
	} `json:"brand"`
	Offers struct {
		Price        string `json:"price"`
		Availability string `json:"availability"`
	} `json:"offers"`
}

// ParseProduct extrae el producto de una ficha de Ahorramas (JSON-LD + HTML).
func ParseProduct(html, url string) (chain.Product, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return chain.Product{}, err
	}

	p := chain.Product{
		Chain:     "ahorramas",
		URL:       url,
		Available: true,
		FetchedAt: time.Now(),
	}

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var ld jsonldProduct
		if err := json.Unmarshal([]byte(s.Text()), &ld); err != nil || ld.Type != "Product" {
			return true
		}
		p.Name = strings.TrimSpace(ld.Name)
		p.SKU = ld.SKU
		p.Brand = strings.TrimSpace(ld.Brand.Name)
		if f, ok := chain.ParsePrice(ld.Offers.Price); ok {
			p.Price = f
		}
		if strings.Contains(strings.ToLower(ld.Offers.Availability), "outofstock") {
			p.Available = false
		}
		return false
	})
	if p.Name == "" {
		return chain.Product{}, errNoProduct
	}

	if m := measureRe.FindStringSubmatch(html); m != nil {
		if f, ok := chain.ParsePrice(m[1]); ok {
			p.MeasurePrice = f
		}
		p.MeasureUnit = canonicalUnit(m[2])
	}
	p.UnitPrice = p.Price

	if old := doc.Find(".unit-price-old, .price-old").First().Text(); old != "" {
		if f, ok := chain.ParsePrice(old); ok {
			p.OldPrice = f
		}
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if promo := promoRe.FindString(text); promo != "" {
		p.PromoText = strings.TrimSpace(promo)
	}

	p.Format = format(doc, p.Name)
	return p, nil
}

func canonicalUnit(raw string) string {
	switch strings.ToUpper(raw) {
	case "KG", "KILO":
		return "kg"
	case "L", "LITRO":
		return "l"
	case "ML":
		return "ml"
	default:
		return strings.ToLower(raw)
	}
}

func format(doc *goquery.Document, name string) string {
	if m := netWeight.FindStringSubmatch(doc.Text()); m != nil {
		return strings.Join(strings.Fields(m[1]), " ")
	}
	if size := chain.FormatFromName(name); size != "" {
		return size
	}
	return ""
}
