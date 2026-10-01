// Package browser ofrece el Chromium headless que comparten las cadenas que no
// se dejan leer con una petición HTTP simple. Lanza un único navegador por
// proceso y reutiliza un perfil temporal.
package browser

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/miferdev/superComparator/internal/config"
)

type Browser struct {
	binary string

	mu          sync.Mutex
	browser     *rod.Browser
	userDataDir string
	launchErr   error
}

func New(cfg config.Config) *Browser {
	return &Browser{binary: cfg.BrowserBin}
}

// Page abre una pestaña en la URL indicada. La devuelve ya cargada; el cierre
// de la pestaña es responsabilidad de quien la pide.
func (b *Browser) Page(ctx context.Context, url string) (*rod.Page, error) {
	r, err := b.connect()
	if err != nil {
		return nil, err
	}
	return r.Context(ctx).Page(proto.TargetCreateTarget{URL: url})
}

func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.browser != nil {
		_ = b.browser.Close()
		b.browser = nil
	}
	if b.userDataDir != "" {
		_ = os.RemoveAll(b.userDataDir)
		b.userDataDir = ""
	}
}

func (b *Browser) connect() (*rod.Browser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.launchErr != nil {
		return nil, b.launchErr
	}
	if b.browser != nil {
		return b.browser, nil
	}
	userDataDir, err := os.MkdirTemp("", "supercomparator-chrome-")
	if err != nil {
		b.launchErr = fmt.Errorf("creando perfil temporal: %w", err)
		return nil, b.launchErr
	}
	l := launcher.New().
		Headless(true).
		Leakless(false).
		Set("no-sandbox").
		Set("disable-dev-shm-usage").
		Set("disable-gpu").
		Set("window-size", "1280,800").
		Set("user-data-dir", userDataDir)
	if b.binary != "" {
		l = l.Bin(b.binary)
	}
	controlURL, err := l.Launch()
	if err != nil {
		_ = os.RemoveAll(userDataDir)
		b.launchErr = fmt.Errorf("lanzando navegador headless: %w", err)
		return nil, b.launchErr
	}
	r := rod.New().ControlURL(controlURL)
	if err := r.Connect(); err != nil {
		_ = os.RemoveAll(userDataDir)
		b.launchErr = fmt.Errorf("conectando al navegador: %w", err)
		return nil, b.launchErr
	}
	b.userDataDir = userDataDir
	b.browser = r
	return r, nil
}
