package mercadona

import (
	"testing"

	"github.com/miferdev/superComparator/internal/config"
)

// Mercadona no enseña la ficha hasta que se fija el código postal, así que el
// adaptador tiene que usar el que le pasa quien lo crea. Se comprobó contra la
// web real: con la config vacía, todas las fichas salen con "producto no
// encontrado" sin decir por qué.
func TestNewGuardaLaConfig(t *testing.T) {
	cfg := config.Config{PostalCode: "28032", Timeout: 30, BrowserBin: "/opt/chrome"}
	c := New(cfg)
	if c.cfg.PostalCode != "28032" || c.cfg.Timeout != 30 || c.cfg.BrowserBin != "/opt/chrome" {
		t.Fatalf("la config no llegó al cliente: %+v", c.cfg)
	}
}
