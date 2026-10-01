package dia

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/miferdev/superComparator/internal/chain"
)

// Fetch descarga una ficha de DÍA y devuelve el producto con su precio y su
// precio por unidad.
func (c *Client) Fetch(ctx context.Context, url string) (chain.Product, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return chain.Product{}, err
	}
	req.Header.Set("User-Agent", chain.UserAgent)
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	// Sin cabecera Accept: DÍA sirve entonces una versión ligera de la página
	// sin los datos estructurados.
	resp, err := c.http.Do(req)
	if err != nil {
		return chain.Product{}, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return chain.Product{}, chain.ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return chain.Product{}, fmt.Errorf("DÍA limita las peticiones: HTTP %d", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return chain.Product{}, fmt.Errorf("HTTP %d en %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return chain.Product{}, err
	}
	p, err := ParseProduct(string(body), url)
	if err != nil {
		return chain.Product{}, chain.ErrNotFound
	}
	return p, nil
}
