package alcampo

import (
	"context"
	"fmt"
	"strings"

	"github.com/miferdev/superComparator/internal/chain"
)

// Fetch abre la ficha en el navegador headless y devuelve el producto. La web
// está detrás de un WAF que puede responder 403 en lugar de la ficha; en ese
// caso la cadena se queda sin precio en vez de inventarse uno.
func (c *Client) Fetch(ctx context.Context, url string) (chain.Product, error) {
	page, err := c.br.Page(ctx, url)
	if err != nil {
		return chain.Product{}, err
	}
	defer page.Close()

	if _, err := page.Timeout(c.timeout).Element(`h1, script[type="application/ld+json"]`); err != nil {
		return chain.Product{}, chain.ErrNotFound
	}
	html, err := page.HTML()
	if err != nil {
		return chain.Product{}, err
	}
	if !strings.Contains(html, "ld+json") {
		if strings.Contains(html, "awswaf") || strings.Contains(html, "waf-captcha") {
			return chain.Product{}, fmt.Errorf("el WAF de Alcampo bloquea la ficha")
		}
		return chain.Product{}, chain.ErrNotFound
	}
	p, err := ParseProduct(html, url)
	if err != nil {
		return chain.Product{}, chain.ErrNotFound
	}
	return p, nil
}
