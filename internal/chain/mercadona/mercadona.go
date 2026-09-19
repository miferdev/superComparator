// Package mercadona implementa el puerto chain sobre las fichas públicas de
// tienda.mercadona.es usando un navegador headless (rod). Solo se accede a
// /sitemap.xml y /product/..., como permite su robots.txt.
package mercadona

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/config"
)

const (
	id         = "mercadona"
	homeURL    = "https://tienda.mercadona.es/"
	sitemapURL = "https://tienda.mercadona.es/sitemap.xml"
	userAgent  = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

type Client struct {
	cfg  config.Config
	http *http.Client

	mu          sync.Mutex
	browser     *rod.Browser
	launchErr   error
	userDataDir string
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
		http: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) ID() string { return id }

func (c *Client) Sitemap(ctx context.Context) ([]chain.SitemapEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
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
	browser, err := c.browserFor(ctx)
	if err != nil {
		return chain.Product{}, err
	}
	page, err := browser.Context(ctx).Page(proto.TargetCreateTarget{URL: url})
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

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.browser != nil {
		_ = c.browser.Close()
		c.browser = nil
	}
	if c.userDataDir != "" {
		_ = os.RemoveAll(c.userDataDir)
		c.userDataDir = ""
	}
}

func (c *Client) browserFor(ctx context.Context) (*rod.Browser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.launchErr != nil {
		return nil, c.launchErr
	}
	if c.browser != nil {
		return c.browser, nil
	}
	userDataDir, err := os.MkdirTemp("", "supercomparator-chrome-")
	if err != nil {
		c.launchErr = fmt.Errorf("creando perfil temporal: %w", err)
		return nil, c.launchErr
	}
	l := launcher.New().
		Headless(true).
		Leakless(false).
		Set("no-sandbox").
		Set("disable-dev-shm-usage").
		Set("disable-gpu").
		Set("window-size", "1280,800").
		Set("user-data-dir", userDataDir)
	if c.cfg.BrowserBin != "" {
		l = l.Bin(c.cfg.BrowserBin)
	}
	controlURL, err := l.Launch()
	if err != nil {
		_ = os.RemoveAll(userDataDir)
		c.launchErr = fmt.Errorf("lanzando navegador headless: %w", err)
		return nil, c.launchErr
	}
	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		_ = os.RemoveAll(userDataDir)
		c.launchErr = fmt.Errorf("conectando al navegador: %w", err)
		return nil, c.launchErr
	}
	c.userDataDir = userDataDir
	c.browser = browser
	return browser, nil
}
