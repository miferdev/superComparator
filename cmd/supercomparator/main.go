// Comando supercomparator: indexa los catálogos de las tiendas y sirve la web
// con los precios comparados.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/miferdev/superComparator/internal/catalog"
	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/chain/ahorramas"
	"github.com/miferdev/superComparator/internal/chain/alcampo"
	"github.com/miferdev/superComparator/internal/chain/dia"
	"github.com/miferdev/superComparator/internal/chain/mercadona"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/store"
)

type flags struct {
	addr       string
	db         string
	browserBin string
	chains     string
	logPath    string
}

// catalogChains son las cuatro tiendas del catálogo, con su ritmo de descarga
// de precios. Alcampo viene con los precios apagados porque su WAF responde 403
// a las fichas: su catálogo sí entra, el precio no.
var catalogChains = []store.Chain{
	{ID: "mercadona", Nombre: "Mercadona", SitemapURL: "https://tienda.mercadona.es/sitemap.xml",
		PreciosActivos: true, PausaSegundos: 2, Concurrencia: 1, PrecioMaxHoras: 24},
	{ID: "ahorramas", Nombre: "Ahorramas", SitemapURL: "https://www.ahorramas.com/sitemap_index.xml",
		PreciosActivos: true, PausaSegundos: 1.5, Concurrencia: 2, PrecioMaxHoras: 24},
	{ID: "dia", Nombre: "DÍA", SitemapURL: "https://www.dia.es/sitemap.xml",
		PreciosActivos: true, PausaSegundos: 1.5, Concurrencia: 2, PrecioMaxHoras: 24},
	{ID: "alcampo", Nombre: "Alcampo", SitemapURL: "https://www.compraonline.alcampo.es/sitemaps/sitemap_index.xml",
		PreciosActivos: false, PausaSegundos: 3, Concurrencia: 1, PrecioMaxHoras: 24},
}

func main() {
	var f flags
	root := &cobra.Command{
		Use:   "supercomparator",
		Short: "Catálogo de precios de Mercadona, Ahorramas, DÍA y Alcampo",
		Long: "Lee los catálogos de las tiendas, guarda nombre, medida y precio de cada " +
			"producto en una base de datos y sirve una web para buscar y comparar.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(loadConfig(&f))
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&f.addr, "addr", "", "dirección donde escuchar (por defecto 127.0.0.1:8080)")
	pf.StringVar(&f.db, "db", "", "ruta de la base SQLite")
	pf.StringVar(&f.browserBin, "browser-bin", "", "binario de Chromium para Mercadona y Alcampo")
	pf.StringVar(&f.chains, "cadenas", "", "cadenas a usar, separadas por comas")
	pf.StringVar(&f.logPath, "log", "", "fichero de log; si no, los logs se descartan")

	root.AddCommand(serveCmd(&f), crawlCmd(&f), smokeCmd(&f), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func loadConfig(f *flags) config.Config {
	cfg := config.Load()
	if f.addr != "" {
		cfg.Addr = f.addr
	}
	if f.db != "" {
		cfg.DBPath = f.db
	}
	if f.browserBin != "" {
		cfg.BrowserBin = f.browserBin
	}
	if f.logPath != "" {
		cfg.LogPath = f.logPath
	}
	if f.chains != "" {
		cfg.Chains = config.SplitChains(f.chains)
	}
	return cfg
}

func loggerFor(cfg config.Config) *slog.Logger {
	if cfg.LogPath == "" {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// selectChains monta los adaptadores de las cadenas pedidas. Si no se pide
// ninguna, monta las cuatro del catálogo.
// selectChains construye los adaptadores de las cadenas pedidas.
//
// La configuración se pasa entera y a propósito: Mercadona necesita el código
// postal para fijar la tienda y su timeout para esperar al render, así que
// inventarse aquí una config con solo el navegador hacía que sus fichas nunca
// llegaran ("producto no encontrado" en todas).
func selectChains(cfg config.Config, pedidas []string) ([]chain.Chain, error) {
	if len(pedidas) == 0 {
		pedidas = []string{"mercadona", "ahorramas", "dia", "alcampo"}
	}
	disponibles := map[string]func() chain.Chain{
		"mercadona": func() chain.Chain { return mercadona.New(cfg) },
		"ahorramas": func() chain.Chain { return ahorramas.New() },
		"dia":       func() chain.Chain { return dia.New() },
		"alcampo":   func() chain.Chain { return alcampo.New(cfg) },
	}
	var out []chain.Chain
	for _, name := range pedidas {
		fabrica, ok := disponibles[name]
		if !ok {
			closeChains(out)
			return nil, fmt.Errorf("cadena desconocida: %s (hay: %s)", name, strings.Join(cadenasConocidas(), ", "))
		}
		out = append(out, fabrica())
	}
	return out, nil
}

func cadenasConocidas() []string {
	return []string{"mercadona", "ahorramas", "dia", "alcampo"}
}

func closeChains(chains []chain.Chain) {
	for _, ch := range chains {
		if c, ok := ch.(interface{ Close() }); ok {
			c.Close()
		}
	}
}

// seedCatalog deja la configuración de las cadenas en la base. Solo rellena lo
// que esté vacío, así que lo que se ajuste a mano sobrevive.
func seedCatalog(st *store.Store) error {
	for _, ch := range catalogChains {
		if err := st.SeedChains([]store.Chain{ch}); err != nil {
			return err
		}
	}
	return nil
}

// runServe es el comando por defecto: levanta la web del catálogo.
func runServe(cfg config.Config) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := seedCatalog(st); err != nil {
		return err
	}
	log := loggerFor(cfg)
	cat := catalog.New(st, log)
	if _, err := cat.Chains(context.Background()); err != nil {
		return err
	}

	// Los precios se rellenan en segundo plano: la web tiene que levantar y
	// poder responder aunque la cola tarde horas. El trabajo vive en la base, así
	// que si el proceso muere al halfway se reanuda sin perder nada.
	ctx := context.Background()
	if precios, err := selectChains(cfg, cfg.Chains); err == nil {
		job := catalog.NewPricesJob(cat, precios)
		go func() {
			res, err := job.Run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("el trabajo de precios paró", "err", err)
			}
			if res.Hechos > 0 || res.Fallidos > 0 {
				log.Info("trabajo de precios terminado",
					"fichas", res.Hechos, "fallidas", res.Fallidos, "duracion", res.Duracion)
			}
		}()
		defer closeChains(precios)
	}

	return newServer(cat, st, log, cfg).ListenAndServe(ctx)
}

// serveCmd es explícito aunque `serve` ya es lo que hace el comando por defecto.
func serveCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Levanta la web del catálogo (lo hace el comando por defecto)",
		RunE:  func(_ *cobra.Command, _ []string) error { return runServe(loadConfig(f)) },
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println(versionStamp()) },
	}
}
