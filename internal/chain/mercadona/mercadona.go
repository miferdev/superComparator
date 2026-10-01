// Package mercadona implementa el puerto chain sobre las fichas públicas de
// tienda.mercadona.es: el sitemap por HTTP y las fichas con un navegador
// headless. Solo se accede a /sitemap.xml y /product/..., como permite su
// robots.txt.
package mercadona

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/chain/browser"
	"github.com/miferdev/superComparator/internal/config"
)

const (
	id         = "mercadona"
	sitemapURL = "https://tienda.mercadona.es/sitemap.xml"
)

type Client struct {
	cfg  config.Config
	http *http.Client
	br   *browser.Browser
}

type urlset struct {
	URLs []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

func New(cfg config.Config) *Client {
	return &Client{
		cfg:  cfg,
		http: chain.NewHTTPClient(45 * time.Second),
		br:   browser.New(cfg),
	}
}

func (c *Client) ID() string { return id }

func (c *Client) Sitemap(ctx context.Context) ([]chain.SitemapEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", chain.UserAgent)
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
	var entries []chain.SitemapEntry
	for _, u := range set.URLs {
		if !strings.Contains(u.Loc, "/product/") {
			continue
		}
		slug := path.Base(u.Loc)
		parts := strings.Split(strings.TrimSuffix(u.Loc, "/"), "/")
		// Category se deja vacía: el slug solo describe el producto
		// ("leche-semidesnatada-hacendado-brick"), no su sección, y adivinar
		// una categoría a partir de él daría resultados falsos.
		e := chain.SitemapEntry{URL: u.Loc, Name: chain.HumanizeSlug(slug)}
		if len(parts) >= 2 {
			e.SKU = parts[len(parts)-2]
		}
		if t, err := time.Parse(time.RFC3339, u.LastMod); err == nil {
			e.LastMod = t
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (c *Client) Fetch(ctx context.Context, url string) (chain.Product, error) {
	page, err := c.br.Page(ctx, url)
	if err != nil {
		return chain.Product{}, err
	}
	defer page.Close()

	const detail = "h1.private-product-detail__description"
	if _, err := page.Timeout(4 * time.Second).Element(detail); err != nil {
		c.setPostalCodeIfNeeded(page)
	}
	if _, err := page.Timeout(c.cfg.Timeout).Element(detail); err != nil {
		return chain.Product{}, chain.ErrNotFound
	}
	html, err := page.HTML()
	if err != nil {
		return chain.Product{}, err
	}
	product, err := ParseProduct(html, url)
	if err != nil {
		return chain.Product{}, chain.ErrNotFound
	}
	return product, nil
}

// setPostalCodeIfNeeded rellena el modal de código postal de Mercadona.
// La cookie de almacén queda fijada para el resto de la sesión del navegador.
func (c *Client) setPostalCodeIfNeeded(page *rod.Page) {
	input, err := page.Timeout(6 * time.Second).Element("input[name=postalCode]")
	if err != nil {
		return
	}
	if err := input.Input(c.cfg.PostalCode); err != nil {
		return
	}
	button, err := page.Timeout(10 * time.Second).Element("button[aria-label=Entrar]")
	if err != nil {
		button, err = page.Timeout(10*time.Second).ElementR("button", "Entrar")
		if err != nil {
			return
		}
	}
	if err := button.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return
	}
	time.Sleep(3 * time.Second)
}

func (c *Client) Close() { c.br.Close() }
