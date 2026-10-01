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
// Con 0,55 se rechazan los productos que solo coinciden en parte del nombre: por
// ejemplo, un "kéfir de fresa" para pedir "kéfir natural".
const AutoThreshold = 0.55

// CoverageThreshold es la fracción mínima (ponderada) de la consulta que debe
// aparecer en el nombre del candidato para entrar en el ranking.
const CoverageThreshold = 0.6

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
	// synonyms unifica términos equivalentes que cada cadena escribe distinto.
	synonyms = map[string]string{
		"refresco": "bebida",
		"gaseosa":  "bebida",
		"soda":     "bebida",
	}
	sizeUnits = map[string]string{
		"kg": "kg", "kilo": "kg", "kilos": "kg",
		"g": "g", "gramo": "g", "gramos": "g",
		"l": "l", "litro": "l", "litros": "l",
		"ml": "ml", "cl": "cl",
		"ud": "ud", "uds": "ud", "unidad": "ud", "unidades": "ud",
	}
)

// Measure es una cantidad normalizada a l (volumen) o kg (peso).
type Measure = chain.Measure

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

// Tokens normaliza, elimina stopwords, aplica stemming ligero de plurales y
// unifica sinónimos.
func Tokens(s string) []string {
	var out []string
	for _, t := range strings.Fields(Normalize(s)) {
		if stopwords[t] {
			continue
		}
		t = stem(t)
		if canonical, ok := synonyms[t]; ok {
			t = canonical
		}
		out = append(out, t)
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

// tokenWeight da más peso a los términos largos, que suelen ser más
// específicos ("desnatada" frente a "sin").
func tokenWeight(t string) float64 {
	return 1 + 0.1*float64(len([]rune(t)))
}

// coverage mide qué parte del peso de la consulta aparece en el candidato.
// A diferencia de Jaccard, no penaliza tokens extra del candidato, de modo que
// la marca o el adjetivo de una cadena no hunden una coincidencia buena.
func coverage(query []string, candidate map[string]bool) float64 {
	total, matched := 0.0, 0.0
	for _, q := range query {
		w := tokenWeight(q)
		total += w
		if candidate[q] {
			matched += w
		}
	}
	if total == 0 {
		return 0
	}
	return matched / total
}

// ParseMeasure extrae una cantidad normalizada de un texto. Entiende packs
// tipo "6 briks x 1 L" (total 6 l) y medidas simples ("400 g").
func ParseMeasure(s string) (Measure, bool) {
	if m := packRe.FindStringSubmatch(s); m != nil {
		count, err1 := strconv.ParseFloat(m[1], 64)
		size, err2 := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", "."), 64)
		if err1 == nil && err2 == nil {
			if measure, ok := chain.NormalizeMeasure(count*size, m[3]); ok {
				return measure, true
			}
		}
	}
	if m := sizeRe.FindStringSubmatch(s); m != nil {
		size, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		if err == nil {
			if measure, ok := chain.NormalizeMeasure(size, m[2]); ok {
				return measure, true
			}
		}
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
		cov := coverage(qt, set)
		if cov < CoverageThreshold {
			continue
		}
		score := cov*2 - 0.02*float64(len(et))
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
// Similarity puntúa cuánto encaja un producto con lo pedido. La medida no cuenta
// como prueba del nombre porque ya se puntúa aparte: si contara, "leche infantil
// de continuación 1 L" encajaría con "leche semidesnatada 1 L" solo por tener el
// mismo litro, que es justo el error que hay que evitar.
func Similarity(query, name, format string) float64 {
	at, bt := nameTokens(query), nameTokens(name)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	set := make(map[string]bool, len(bt))
	for _, t := range bt {
		set[t] = true
	}
	score := coverage(at, set)

	qm, qok := ParseMeasure(query)
	pm, pok := ParseMeasure(format)
	if !pok {
		pm, pok = ParseMeasure(name)
	}
	if qok && pok && !sameMeasure(qm, pm) {
		// Un formato parecido se penaliza poco; uno muy distinto (un pack de
		// 13 L para pedir 1 L), mucho: no es el producto que se pidió.
		if measureRatio(qm, pm) <= 2 {
			score -= 0.3
		} else {
			score -= 0.55
		}
	}
	if headMismatch(at, bt) {
		// El producto no es el que se pidió: es otro que lo lleva dentro.
		score *= 0.4
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

// headMismatch avisa de que el candidato es otro producto que contiene lo que
// se pidió. En las tiendas el sustantivo principal va el primero («Mermelada de
// fresa», «Pipas de calabaza»), así que si esa primera palabra no está entre las
// que se han pedido, no es el producto buscado: es otro que lo lleva dentro.
func headMismatch(queryTokens, nameTokens []string) bool {
	if len(queryTokens) == 0 || len(nameTokens) == 0 {
		return false
	}
	pedidos := make(map[string]bool, len(queryTokens))
	for _, t := range queryTokens {
		pedidos[t] = true
	}
	return !pedidos[nameTokens[0]]
}

// nameTokens descarta los tokens que son solo una medida. Si el nombre se queda
// sin nada útil, se conservan todos.
func nameTokens(s string) []string {
	all := Tokens(s)
	var out []string
	for _, t := range all {
		if _, ok := ParseMeasure(t); ok {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return all
	}
	return out
}

// InOrder devuelve las entradas ya ordenadas por quien las ha buscado, sin
// volver a puntuarlas. Lo usan las cadenas que filtran el catálogo ellas
// mismas, porque sus nombres no siempre encajan con el criterio de Rank.
func InOrder(entries []chain.SitemapEntry) []Scored {
	out := make([]Scored, 0, len(entries))
	for _, e := range entries {
		out = append(out, Scored{Entry: e})
	}
	return out
}

// measureRatio indica cuánto mayor es un formato que otro. Si las unidades no
// son comparables devuelve un valor enorme: kg frente a litros no es el mismo
// producto.
func measureRatio(a, b Measure) float64 {
	if a.Value <= 0 || b.Value <= 0 {
		return 0
	}
	if a.Unit != b.Unit {
		return math.Inf(1)
	}
	alto, bajo := math.Max(a.Value, b.Value), math.Min(a.Value, b.Value)
	return alto / bajo
}
