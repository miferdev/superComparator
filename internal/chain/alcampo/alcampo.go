// Package alcampo implementa el puerto chain contra compraonline.alcampo.es:
// los sitemaps de producto se leen por HTTP y las fichas con un navegador
// headless, porque su WAF no responde a peticiones HTTP simples.
//
// Su catálogo se resuelve por sitemap: el slug de cada URL ya describe el
// producto ("leche-entera-de-vaca-1-l"), así que el nombre legible sale de ahí.
// Solo se usan rutas permitidas por su robots.txt (/sitemaps y /products).
package alcampo

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/chain/browser"
	"github.com/miferdev/superComparator/internal/config"
)

const (
	id          = "alcampo"
	baseURL     = "https://www.compraonline.alcampo.es"
	sitemapIdx  = baseURL + "/sitemaps/sitemap_index.xml"
	sitemapSize = 24 << 20
)

type Client struct {
	http    *http.Client
	br      *browser.Browser
	timeout time.Duration

	mu      sync.Mutex
	entries []chain.SitemapEntry
}

type sitemapIndex struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

type urlset struct {
	URLs []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

func New(cfg config.Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &Client{
		http:    chain.NewHTTPClient(90 * time.Second),
		br:      browser.New(cfg),
		timeout: timeout,
	}
}

func (c *Client) ID() string { return id }

func (c *Client) Sitemap(ctx context.Context) ([]chain.SitemapEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries != nil {
		return c.entries, nil
	}
	body, err := c.get(ctx, sitemapIdx)
	if err != nil {
		return nil, fmt.Errorf("sitemap index: %w", err)
	}
	var idx sitemapIndex
	if err := xml.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parseando sitemap index: %w", err)
	}
	entries := make([]chain.SitemapEntry, 0, 60000)
	for _, s := range idx.Sitemaps {
		if !strings.Contains(s.Loc, "product") {
			continue
		}
		page, err := c.get(ctx, s.Loc)
		if err != nil {
			return nil, fmt.Errorf("sitemap %s: %w", s.Loc, err)
		}
		var set urlset
		if err := xml.Unmarshal(page, &set); err != nil {
			return nil, fmt.Errorf("parseando %s: %w", s.Loc, err)
		}
		for _, u := range set.URLs {
			if !strings.Contains(u.Loc, "/products/") {
				continue
			}
			entries = append(entries, entryFromURL(u.Loc, u.LastMod))
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("los sitemaps no traen productos")
	}
	c.entries = entries
	return entries, nil
}

func (c *Client) Close() { c.br.Close() }

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("HTTP %d en %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, sitemapSize))
}

// entryFromURL construye la entrada del catálogo a partir de
// /products/larsа-leche-entera-1-l/51814.
func entryFromURL(rawURL, lastMod string) chain.SitemapEntry {
	segments := strings.Split(strings.TrimSuffix(rawURL, "/"), "/")
	e := chain.SitemapEntry{URL: rawURL}
	if n := len(segments); n >= 2 {
		e.SKU = segments[n-1]
		e.Name = chain.HumanizeSlug(segments[n-2])
	}
	if t, err := time.Parse(time.RFC3339, lastMod); err == nil {
		e.LastMod = t
	}
	return e
}
