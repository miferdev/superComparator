package chain

import (
	"regexp"
	"strconv"
	"strings"
)

// Measure es una cantidad normalizada a una base canónica: kg para masa y l
// para volumen.
type Measure struct {
	Value float64
	Unit  string
}

var measurePriceRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(?:€|&euro;|eur)\s*/\s*([a-z]+)\b`)

// NormalizeMeasure convierte una cantidad con unidad a su base canónica:
// gramos a kg y mililitros/centilitros a litros.
func NormalizeMeasure(value float64, unit string) (Measure, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "kg", "kilo", "kilos":
		return Measure{value, "kg"}, true
	case "g", "gramo", "gramos":
		return Measure{value / 1000, "kg"}, true
	case "l", "litro", "litros":
		return Measure{value, "l"}, true
	case "ml", "mililitro", "mililitros":
		return Measure{value / 1000, "l"}, true
	case "cl", "centilitro", "centilitros":
		return Measure{value / 100, "l"}, true
	case "ud", "uds", "unidad", "unidades":
		return Measure{value, "ud"}, true
	}
	return Measure{}, false
}

// ParseMeasurePrice extrae un precio por unidad ("2,45 €/kg", "2,50&euro;/KG")
// y lo normaliza a €/kg o €/l. Los precios por unidad suelta (/ud) no son una
// medida continua y se descartan.
func ParseMeasurePrice(text string) (Measure, bool) {
	for _, m := range measurePriceRe.FindAllStringSubmatch(text, -1) {
		price, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		if err != nil || price <= 0 {
			continue
		}
		unit, ok := NormalizeMeasure(1, m[2])
		if !ok || unit.Unit == "ud" {
			continue
		}
		return Measure{Value: price / unit.Value, Unit: unit.Unit}, true
	}
	return Measure{}, false
}
