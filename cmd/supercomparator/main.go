// Comando supercomparator: procesa lista.md y escribe el informe markdown.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/chain/ahorramas"
	"github.com/miferdev/superComparator/internal/chain/mercadona"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/report"
	"github.com/miferdev/superComparator/internal/store"
)

const version = "0.1.0"

type flags struct {
	cp         string
	lista      string
	db         string
	reportPath string
	browserBin string
	workers    int
	candidates int
	delayMS    int
	mercadonaU string
	ahorramasU string
}

func main() {
	var f flags
	root := &cobra.Command{
		Use:   "supercomparator",
		Short: "Compara precios de la compra entre Mercadona y Ahorramas",
		Long: "Procesa la lista de la compra (lista.md), resuelve cada producto en las " +
			"cadenas configuradas y escribe el informe markdown con la opción más barata.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(loadConfig(&f))
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&f.cp, "cp", "", "código postal (por defecto 28032)")
	pf.StringVar(&f.lista, "lista", "", "ruta de lista.md (por defecto lista.md)")
	pf.StringVar(&f.db, "db", "", "ruta de la base SQLite")
	pf.StringVar(&f.reportPath, "report", "", "ruta del informe markdown")
	pf.StringVar(&f.browserBin, "browser-bin", "", "binario de Chromium para Mercadona")
	pf.IntVar(&f.workers, "workers", 0, "peticiones en paralelo")
	pf.IntVar(&f.candidates, "candidates", 0, "candidatos por cadena")
	pf.IntVar(&f.delayMS, "delay-ms", 0, "pausa entre peticiones (ms)")

	root.AddCommand(checkCmd(&f), reportCmd(&f), smokeCmd(&f), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run es el flujo por defecto: resolver lo que falte, comprobar precios y
// escribir el informe markdown.
func run(cfg config.Config) error {
	if cfg.ListaPath == "" {
		cfg.ListaPath = "lista.md"
	}
	items, err := list.ParseFile(cfg.ListaPath)
	if err != nil {
		return err
	}
	c, cleanup, err := build(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	matches, _ := c.Store().Matches()
	if needsResolve(matches, items, len(c.Chains())) {
		fmt.Println("Resolviendo productos…")
		if err := c.Resolve(ctx, items, printEvent); err != nil {
			return err
		}
	}
	fmt.Println("Comprobando precios…")
	if err := c.Check(ctx, printEvent); err != nil {
		return err
	}
	cmp, err := c.Comparison(ctx)
	if err != nil {
		return err
	}
	if err := report.Write(cfg.ReportPath, cmp); err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(report.Console(cmp))
	fmt.Printf("Informe: %s\n", cfg.ReportPath)
	return nil
}

func loadConfig(f *flags) config.Config {
	cfg := config.Load()
	if f.cp != "" {
		cfg.PostalCode = f.cp
	}
	if f.lista != "" {
		cfg.ListaPath = f.lista
	}
	if f.db != "" {
		cfg.DBPath = f.db
		if f.reportPath == "" {
			cfg.ReportPath = filepath.Join(filepath.Dir(cfg.DBPath), "informe.md")
		}
	}
	if f.reportPath != "" {
		cfg.ReportPath = f.reportPath
	}
	if f.browserBin != "" {
		cfg.BrowserBin = f.browserBin
	}
	if f.workers > 0 {
		cfg.Workers = f.workers
	}
	if f.candidates > 0 {
		cfg.Candidates = f.candidates
	}
	if f.delayMS > 0 {
		cfg.Delay = time.Duration(f.delayMS) * time.Millisecond
	}
	return cfg
}

func build(cfg config.Config) (*core.Core, func(), error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	mc := mercadona.New(cfg)
	chains := []chain.Chain{mc, ahorramas.New()}
	c := core.New(cfg, chains, st, loggerFor(cfg))
	cleanup := func() {
		mc.Close()
		st.Close()
	}
	return c, cleanup, nil
}

func loggerFor(cfg config.Config) *slog.Logger {
	if cfg.LogPath == "" {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func checkCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:        "check",
		Short:      "Resuelve y comprueba la lista sin imprimir la comparativa",
		Deprecated: "usa el comando principal: supercomparator",
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(loadConfig(f))
		},
	}
}

func reportCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Regenera el informe a partir de los precios guardados",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadConfig(f)
			c, cleanup, err := build(cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			cmp, err := c.Comparison(context.Background())
			if err != nil {
				return err
			}
			if err := report.Write(cfg.ReportPath, cmp); err != nil {
				return err
			}
			fmt.Print(report.Console(cmp))
			fmt.Println("\nInforme:", cfg.ReportPath)
			return nil
		},
	}
}

func smokeCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smoke",
		Short: "Descarga una ficha de cada cadena (diagnóstico)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadConfig(f)
			ctx := context.Background()
			mc := mercadona.New(cfg)
			defer mc.Close()
			ah := ahorramas.New()

			for _, probe := range []struct {
				ch  chain.Chain
				url string
			}{
				{mc, f.mercadonaU},
				{ah, f.ahorramasU},
			} {
				p, err := probe.ch.Fetch(ctx, probe.url)
				if err != nil {
					return fmt.Errorf("%s: %w", probe.ch.ID(), err)
				}
				out, _ := json.MarshalIndent(p, "", "  ")
				fmt.Printf("%s\n", out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.mercadonaU, "mercadona-url", "https://tienda.mercadona.es/product/10005/chocolate-liquido-taza-hacendado-brick", "url de prueba de Mercadona")
	cmd.Flags().StringVar(&f.ahorramasU, "ahorramas-url", "https://www.ahorramas.com/garbanzo-cocido-luengo-400g-44569.html", "url de prueba de Ahorramas")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println("supercomparator", version) },
	}
}

// needsResolve indica si hay que resolver de nuevo. Compara los nombres de la
// lista con los ya resueltos: si falta alguno, sobran algunos del histórico o
// alguno quedó con una coincidencia dudosa.
func needsResolve(matches []store.Match, items []list.Item, chains int) bool {
	resueltos := make(map[string]bool, len(matches))
	for _, m := range matches {
		resueltos[m.ItemName] = true
	}
	for _, it := range items {
		if !resueltos[it.Name] {
			return true
		}
	}
	if len(resueltos) != len(items) {
		return true
	}
	for _, m := range matches {
		if m.Score < match.AutoThreshold {
			return true
		}
	}
	return false
}

func printEvent(e core.Event) {
	switch ev := e.(type) {
	case core.ItemStarted:
		fmt.Printf("[%d/%d] %s\n", ev.Index, ev.Total, ev.Item)
	case core.ChainResolved:
		fmt.Printf("  %s → %s (%.2f €)\n", report.ChainName(ev.Chain), ev.Product.Name, ev.Product.Price)
	case core.ItemNeedsReview:
		fmt.Printf("  ¡revisar! %s: confianza baja\n", ev.Item)
	case core.ItemFailed:
		fmt.Printf("  %s: %s\n", report.ChainName(ev.Chain), ev.Err)
	case core.PriceChanged:
		fmt.Printf("  %s: %.2f € → %.2f €\n", report.ChainName(ev.Chain), ev.Old, ev.New)
	case core.ProductDelisted:
		fmt.Printf("  descatalogado en %s: %s\n", report.ChainName(ev.Chain), ev.Item)
	}
}
