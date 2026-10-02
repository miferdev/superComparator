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
		Price         chain.JSONText `json:"price"`
		PriceCurrency string         `json:"priceCurrency"`
		Availability  string         `json:"availability"`
	} `json:"offers"`
}

// jsonldBreadcrumb es la miga de pan que publica Ahorramas en un JSON-LD aparte:
// los niveles van en itemListElement[].item.name, con la ruta de la sección.
type jsonldBreadcrumb struct {
	Type            string `json:"@type"`
	ItemListElement []struct {
		Name string `json:"name"`
		Item struct {
			Name string `json:"name"`
		} `json:"item"`
	} `json:"itemListElement"`
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
		// El JSON-LD es texto plano para el parser de HTML, así que sus entidades
		// llegan sin desescapar ("Hellmann&#039;s") y se guardan tal cual: hay que
		// desescaparlas aquí, y solo aquí, porque el resto de la ficha sale del DOM y
		// viene ya desescapado.
		p.Name = chain.UnescapeText(strings.TrimSpace(ld.Name))
		p.SKU = ld.SKU
		p.Brand = chain.UnescapeText(strings.TrimSpace(ld.Brand.Name))
		// El precio del JSON-LD viene en formato máquina ("1.65"), con punto decimal
		// y sin separador de miles, así que se lee como número JSON: ParsePrice es
		// para el texto en español de la web ("1,65 €") y corta en el punto, con lo
		// que un 1,65 se guardaba como 1 y el 40 % del precio se perdía en silencio.
		if f, ok := chain.ParseJSONNumber(string(ld.Offers.Price)); ok {
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
	p.Category = categoryFromBreadcrumb(doc)

	// Ahorramas publica el precio de unidad en el JSON-LD y el precio por
	// medida aparte. Si el propio precio viene por kilo es que el producto se
	// vende al peso: entonces no hay precio de unidad y Price se deja a 0, en
	// lugar de servir "1,65 €" por una bolsa.
	precioPublicado := chain.NormalizeSpaces(doc.Find(".price .sales").First().Text())
	if m, ok := chain.ParseMeasurePrice(precioPublicado); ok {
		p.Price = 0
		p.UnitPrice = 0
		p.PriceIsPerMeasure = true
		p.MeasurePrice = m.Value
		p.MeasureUnit = m.Unit
	} else {
		p.UnitPrice = p.Price
		if m, ok := measurePriceFrom(doc); ok {
			p.MeasurePrice = m.Value
			p.MeasureUnit = m.Unit
		}
		if p.Price <= 0 && p.MeasurePrice > 0 {
			// Sin precio de unidad en el JSON-LD lo único publicado es el de la
			// medida: tampoco hay un precio de unidad que sacar de ahí.
			p.PriceIsPerMeasure = true
		}
	}

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

func format(doc *goquery.Document, name string) string {
	if m := netWeight.FindStringSubmatch(doc.Text()); m != nil {
		return strings.Join(strings.Fields(m[1]), " ")
	}
	if size := chain.FormatFromName(name); size != "" {
		return size
	}
	return ""
}

// measurePriceFrom busca el precio por medida aparte ("2,50 €/KG.PESO ESC").
// Se recorren todos los .unit-price-per-unit porque el primero de la ficha es el
// precio anterior tachado, que no lleva unidad.
func measurePriceFrom(doc *goquery.Document) (chain.Measure, bool) {
	var medida chain.Measure
	var ok bool
	doc.Find(".unit-price-per-unit").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		medida, ok = chain.ParseMeasurePrice(s.Text())
		return !ok
	})
	return medida, ok
}

// categoryFromBreadcrumb saca la sección del producto del BreadcrumbList del
// JSON-LD ("Alimentación / Arroces, Pastas y Legumbres / Legumbres"). El
// nombre del producto no dice de qué sección es, así que sin esto el catálogo
// solo tendría la categoría que se haya deducido del slug.
func categoryFromBreadcrumb(doc *goquery.Document) string {
	var niveles []string
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var bc jsonldBreadcrumb
		if err := json.Unmarshal([]byte(s.Text()), &bc); err != nil {
			return true
		}
		if !strings.EqualFold(strings.TrimSpace(bc.Type), "BreadcrumbList") {
			return true
		}
		for _, nivel := range bc.ItemListElement {
			// La miga también viene del JSON-LD, con sus entidades sin desescapar.
			nombre := chain.UnescapeText(strings.TrimSpace(nivel.Item.Name))
			if nombre == "" {
				nombre = chain.UnescapeText(strings.TrimSpace(nivel.Name))
			}
			niveles = append(niveles, nombre)
		}
		return false
	})
	return chain.CategoryPath(niveles...)
}
