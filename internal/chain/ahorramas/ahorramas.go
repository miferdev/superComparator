// Package ahorramas implementa el puerto chain contra la tienda online de
// Ahorramas: sitemap de productos y fichas server-rendered (JSON-LD + HTML).
package ahorramas

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
)

const (
	id        = "ahorramas"
	baseURL   = "https://www.ahorramas.com"
	sitemapIn = baseURL + "/sitemap_index.xml"
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

var skuRe = regexp.MustCompile(`-(\d+)\.html$`)

type Client struct {
	http *http.Client
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

func New() *Client {
	return &Client{http: &http.Client{Timeout: 45 * time.Second}}
}

func (c *Client) ID() string { return id }

func (c *Client) Sitemap(ctx context.Context) ([]chain.SitemapEntry, error) {
	index, err := c.get(ctx, sitemapIn)
	if err != nil {
		return nil, fmt.Errorf("sitemap index: %w", err)
	}
	var idx sitemapIndex
	if err := xml.Unmarshal(index, &idx); err != nil {
		return nil, fmt.Errorf("parseando sitemap index: %w", err)
	}
	productSitemap := ""
	for _, s := range idx.Sitemaps {
		if strings.Contains(s.Loc, "product") {
			productSitemap = s.Loc
			break
		}
	}
	if productSitemap == "" {
		return nil, errors.New("no hay sitemap de productos")
	}
	body, err := c.get(ctx, productSitemap)
	if err != nil {
		return nil, fmt.Errorf("sitemap productos: %w", err)
	}
	var set urlset
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("parseando sitemap de productos: %w", err)
	}
	entries := make([]chain.SitemapEntry, 0, len(set.URLs))
	for _, u := range set.URLs {
		slug := strings.TrimSuffix(path.Base(u.Loc), ".html")
		e := chain.SitemapEntry{
			URL:  u.Loc,
			Name: chain.HumanizeSlug(slug),
			SKU:  sku(slug),
		}
		if t, err := time.Parse(time.RFC3339, u.LastMod); err == nil {
			e.LastMod = t
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (c *Client) Fetch(ctx context.Context, rawURL string) (chain.Product, error) {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return chain.Product{}, err
	}
	p, err := ParseProduct(string(body), rawURL)
	if errors.Is(err, errNoProduct) {
		return chain.Product{}, chain.ErrNotFound
	}
	if err != nil {
		return chain.Product{}, err
	}
	return p, nil
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, chain.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d en %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func sku(slug string) string {
	if m := skuRe.FindStringSubmatch(slug + ".html"); m != nil {
		return m[1]
	}
	return ""
}
