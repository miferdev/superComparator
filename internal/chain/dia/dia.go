package dia

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/match"
)

const (
	id         = "dia"
	sitemapURL = "https://www.dia.es/sitemap.xml"
)

type Client struct {
	http *http.Client

	mu          sync.Mutex
	entries     []chain.SitemapEntry
	byCategory  map[string][]chain.SitemapEntry
	categories  []chain.SitemapEntry
	lookupCache map[string][]chain.SitemapEntry
}

type urlset struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

func New() *Client {
	return &Client{
		http:        chain.NewHTTPClient(45 * time.Second),
		byCategory:  make(map[string][]chain.SitemapEntry),
		lookupCache: make(map[string][]chain.SitemapEntry),
	}
}

func (c *Client) ID() string { return id }

func (c *Client) Sitemap(ctx context.Context) ([]chain.SitemapEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries != nil {
		return c.entries, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", chain.UserAgent)
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d en sitemap", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var set urlset
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("parseando sitemap: %w", err)
	}
	entries := make([]chain.SitemapEntry, 0, len(set.URLs))
	for _, u := range set.URLs {
		if !strings.Contains(u.Loc, "/p/") {
			continue
		}
		cat := categoryFromURL(u.Loc)
		sub := path.Base(cat)
		e := chain.SitemapEntry{URL: u.Loc, Name: chain.HumanizeSlug(sub), SKU: productID(u.Loc)}
		entries = append(entries, e)
		c.byCategory[cat] = append(c.byCategory[cat], e)
	}
	for cat := range c.byCategory {
		c.categories = append(c.categories, chain.SitemapEntry{URL: cat, Name: chain.HumanizeSlug(path.Base(cat))})
	}
	sort.Slice(c.categories, func(i, j int) bool { return c.categories[i].URL < c.categories[j].URL })
	c.entries = entries
	return entries, nil
}

// Lookup devuelve las fichas de las categorías que mejor encajan con la
// consulta. Como el sitemap no trae el nombre del producto, se muestrea un
// número acotado de fichas por categoría para que el núcleo pueda compararlas
// con el nombre real de cada ficha.
func (c *Client) Lookup(ctx context.Context, query string) ([]chain.SitemapEntry, error) {
	if _, err := c.Sitemap(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.lookupCache[query]; ok {
		return cached, nil
	}
	// Las categorías se puntúan por parecido y no con Rank: sus nombres son
	// genéricos ("leche") y no cubren todos los tokens de la consulta.
	type scoredCat struct {
		url    string
		score  float64
		tokens int
	}
	scored := make([]scoredCat, 0, len(c.categories))
	for _, cat := range c.categories {
		if s := categoryScore(query, cat.Name); s >= minCategoryScore {
			scored = append(scored, scoredCat{url: cat.URL, score: s, tokens: len(match.Tokens(cat.Name))})
		}
	}
	// A igual cobertura gana la categoría más específica: para "Leche entera 1L"
	// encaja mejor "leche" que "chocolate con leche".
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].tokens < scored[j].tokens
	})
	if len(scored) > maxCategories {
		scored = scored[:maxCategories]
	}
	var out []chain.SitemapEntry
	for _, cat := range scored {
		out = append(out, sample(c.byCategory[cat.url], sampleSize)...)
	}
	c.lookupCache[query] = out
	return out, nil
}

// categoryScore mide qué parte de la consulta aparece en el nombre de una
// categoría, con la misma ponderación por longitud que usa match.
func categoryScore(query, name string) float64 {
	qt, nt := match.Tokens(query), match.Tokens(name)
	if len(qt) == 0 || len(nt) == 0 {
		return 0
	}
	total, matched := 0.0, 0.0
	for _, q := range qt {
		total++
		for _, n := range nt {
			if q == n || sameWord(q, n) {
				matched++
				break
			}
		}
	}
	return matched / total
}

// sameWord tolera que la lematización de match deje variantes distintas de la
// misma palabra: "aceite" y "aceites" no coinciden, pero sí son la misma.
func sameWord(a, b string) bool {
	const minLen = 4
	if len(a) < minLen || len(b) < minLen {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// maxCategories es cuántas categorías distintas se muestrean por consulta y
// sampleSize cuántas fichas ofrece de cada una. Se ajustan al ranking por
// defecto del núcleo (15 candidatos) porque la cadena no recibe su configuración.
const (
	maxCategories = 3
	sampleSize    = 5
	// minCategoryScore descarta categorías que solo comparten una palabra
	// suelta con la consulta, que más que productos traen otra cosa.
	minCategoryScore = 0.25
)

// sample reparte las fichas de una categoría a lo largo de su listado, para no
// quedarte siempre con los mismos productos.
func sample(entries []chain.SitemapEntry, n int) []chain.SitemapEntry {
	if len(entries) <= n {
		return entries
	}
	step := float64(len(entries)) / float64(n)
	out := make([]chain.SitemapEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, entries[int(float64(i)*step)])
	}
	return out
}
