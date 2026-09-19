// Package match normaliza texto (acentos, stopwords, plurales) y puntúa
// productos de los sitemaps contra una consulta de la lista de la compra.
package match

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/miferdev/superComparator/internal/chain"
)

// AutoThreshold es la similitud mínima para aceptar un producto sin revisión.
const AutoThreshold = 0.5

var (
	sizeRe    = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(kg|kilos?|g|gramos?|l|litros?|ml|cl|uds?|unidades?)\b`)
	packRe    = regexp.MustCompile(`(?i)(\d+)\s+(?:[\p{L}]+\s+)*[x×]\s*(\d+(?:[.,]\d+)?)\s*(ml|cl|l|kg|g)\b`)
	nonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	spaceRe   = regexp.MustCompile(`\s+`)
	stopwords = map[string]bool{
		"de": true, "del": true, "la": true, "el": true, "los": true, "las": true,
		"un": true, "una": true, "unos": true, "unas": true, "con": true, "sin": true,
		"y": true, "e": true, "o": true, "u": true, "en": true, "para": true,
		"por": true, "al": true, "a": true,
	}
	packWords = map[string]bool{"pack": true, "lote": true, "estuche": true, "multipack": true}
	sizeUnits = map[string]string{
		"kg": "kg", "kilo": "kg", "kilos": "kg",
		"g": "g", "gramo": "g", "gramos": "g",
		"l": "l", "litro": "l", "litros": "l",
		"ml": "ml", "cl": "cl",
		"ud": "ud", "uds": "ud", "unidad": "ud", "unidades": "ud",
	}
)

// Measure es una cantidad normalizada a l (volumen) o kg (peso).
type Measure struct {
	Value float64
	Unit  string
}

type Scored struct {
	Entry chain.SitemapEntry
	Score float64
}

// Normalize deja el texto en minúsculas, sin acentos, con números y unidades
// pegados ("1 l" -> "1l") y separado por espacios simples.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = string(norm.NFD.AppendString(nil, s))
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	s = sizeRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := sizeRe.FindStringSubmatch(m)
		number := strings.ReplaceAll(parts[1], ",", ".")
		unit := strings.ToLower(parts[2])
		if u, ok := sizeUnits[unit]; ok {
			unit = u
		}
		return number + unit
	})
	s = nonAlnum.ReplaceAllString(s, " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// Tokens normaliza, elimina stopwords y aplica stemming ligero de plurales.
func Tokens(s string) []string {
	var out []string
	for _, t := range strings.Fields(Normalize(s)) {
		if stopwords[t] {
			continue
		}
		out = append(out, stem(t))
	}
	return out
}

func stem(t string) string {
	if len(t) > 4 && strings.HasSuffix(t, "es") {
		return strings.TrimSuffix(t, "es")
	}
	if len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") {
		return strings.TrimSuffix(t, "s")
	}
	return t
}

// ParseMeasure extrae una cantidad normalizada de un texto. Entiende packs
// tipo "6 briks x 1 L" (total 6 l) y medidas simples ("400 g").
func ParseMeasure(s string) (Measure, bool) {
	if m := packRe.FindStringSubmatch(s); m != nil {
		count, err1 := strconv.ParseFloat(m[1], 64)
		size, err2 := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", "."), 64)
		if err1 == nil && err2 == nil {
			if measure, ok := normalizeMeasure(count*size, m[3]); ok {
				return measure, true
			}
		}
	}
	if m := sizeRe.FindStringSubmatch(s); m != nil {
		size, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		if err == nil {
			if measure, ok := normalizeMeasure(size, m[2]); ok {
				return measure, true
			}
		}
	}
	return Measure{}, false
}

func normalizeMeasure(value float64, unit string) (Measure, bool) {
	switch strings.ToLower(unit) {
	case "ml":
		return Measure{value / 1000, "l"}, true
	case "cl":
		return Measure{value / 10, "l"}, true
	case "l", "litro", "litros":
		return Measure{value, "l"}, true
	case "g", "gramo", "gramos":
		return Measure{value / 1000, "kg"}, true
	case "kg", "kilo", "kilos":
		return Measure{value, "kg"}, true
	}
	return Measure{}, false
}

func sameMeasure(a, b Measure) bool {
	if a.Unit != b.Unit || a.Value <= 0 || b.Value <= 0 {
		return false
	}
	return math.Abs(a.Value-b.Value)/math.Max(a.Value, b.Value) <= 0.1
}

func isPack(s string) bool {
	low := strings.ToLower(s)
	for word := range packWords {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

// Rank puntúa las entradas del sitemap contra la consulta y devuelve las
// mejores. Exige cubrir al menos el 60% de los tokens de la consulta.
func Rank(query string, entries []chain.SitemapEntry, limit int) []Scored {
	qt := Tokens(query)
	if len(qt) == 0 || len(entries) == 0 {
		return nil
	}
	queryHasPack := false
	for _, q := range qt {
		if packWords[q] {
			queryHasPack = true
		}
	}
	phrase := Normalize(strings.Join(qt, " "))
	seen := make(map[string]bool, len(entries))
	out := make([]Scored, 0, len(entries))
	for _, e := range entries {
		if seen[e.URL] {
			continue
		}
		et := Tokens(e.Name)
		if len(et) == 0 {
			continue
		}
		set := make(map[string]bool, len(et))
		for _, t := range et {
			set[t] = true
		}
		matched := 0
		for _, q := range qt {
			if set[q] {
				matched++
			}
		}
		coverage := float64(matched) / float64(len(qt))
		if coverage < 0.6 {
			continue
		}
		score := coverage*2 - 0.02*float64(len(et))
		if strings.Contains(Normalize(e.Name), phrase) {
			score += 0.5
		}
		if !queryHasPack {
			for _, t := range et {
				if packWords[t] {
					score -= 0.6
					break
				}
			}
		}
		seen[e.URL] = true
		out = append(out, Scored{Entry: e, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return len(out[i].Entry.Name) < len(out[j].Entry.Name)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Similarity compara la consulta con el nombre y el formato de un producto.
// Penaliza los packs cuando la lista no los pide y premia la misma cantidad.
func Similarity(query, name, format string) float64 {
	at, bt := Tokens(query), Tokens(name)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	set := make(map[string]bool, len(bt))
	for _, t := range bt {
		set[t] = true
	}
	inter := 0
	for _, t := range at {
		if set[t] {
			inter++
		}
	}
	union := len(at) + len(bt) - inter
	if union == 0 {
		return 0
	}
	score := float64(inter) / float64(union)

	qm, qok := ParseMeasure(query)
	pm, pok := ParseMeasure(format)
	if !pok {
		pm, pok = ParseMeasure(name)
	}
	switch {
	case qok && pok && sameMeasure(qm, pm):
		score += 0.2
	case qok && pok:
		score -= 0.3
	}
	if !strings.Contains(strings.ToLower(query), "pack") && isPack(format+" "+name) {
		score -= 0.1
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
