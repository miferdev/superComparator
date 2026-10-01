// Comando supercomparator: procesa lista.md y escribe el informe markdown.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/match"
	"github.com/miferdev/superComparator/internal/report"
	"github.com/miferdev/superComparator/internal/store"
	"github.com/miferdev/superComparator/internal/version"
)

// defaultChains son las cadenas que se comparan si no se pide ninguna.
var defaultChains = []string{"mercadona", "ahorramas", "dia"}

type flags struct {
	cp         string
	lista      string
	db         string
	reportPath string
	browserBin string
	workers    int
	candidates int
	delayMS    int
	chains     string
	mercadonaU string
	ahorramasU string
	diaU       string
	alcampoU   string
}

func main() {
	var f flags
	root := &cobra.Command{
		Use:   "supercomparator",
		Short: "Compara precios de la compra entre Mercadona, Ahorramas y DÍA",
		Long: "Procesa la lista de la compra (lista.md), resuelve cada producto en las " +
			"cadenas configuradas y escribe un informe markdown por supermercado y otro " +
			"con la opción más barata de cada producto.",
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
	pf.StringVar(&f.browserBin, "browser-bin", "", "binario de Chromium para Mercadona y Alcampo")
	pf.StringVar(&f.chains, "cadenas", "", "cadenas a comparar, separadas por comas (por defecto mercadona,ahorramas,dia)")
	pf.IntVar(&f.workers, "workers", 0, "peticiones en paralelo")
	pf.IntVar(&f.candidates, "candidates", 0, "candidatos por cadena")
	pf.IntVar(&f.delayMS, "delay-ms", 0, "pausa entre peticiones (ms)")

	root.AddCommand(checkCmd(&f), reportCmd(&f), explainCmd(&f), smokeCmd(&f), versionCmd())
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
	forzar, err := reglasCambiadas(c.Store())
	if err != nil {
		return err
	}
	if forzar || needsResolve(matches, items, c.Chains()) {
		if forzar {
			fmt.Println("Las reglas de coincidencia han cambiado: se resuelve la lista entera otra vez.")
			// Hay que vaciar lo anterior: si no, la resolución lo daría por
			// hecho y se mezclarían productos de dos versiones de las reglas.
			if err := c.Store().ClearMatches(); err != nil {
				return err
			}
		}
		fmt.Println("Resolviendo productos…")
		if err := c.Resolve(ctx, items, printEvent); err != nil {
			return err
		}
		if err := c.Store().SetMeta(metaMatchVersion, version.Matching); err != nil {
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
	written, err := report.WriteAll(cfg.ReportPath, cmp)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(report.Console(cmp))
	printFiles(written)
	return nil
}

// printFiles informa de los informes escritos. WriteAll deja el comparativo
// el último, así que se distingue de los informes por cadena por la posición.
func printFiles(written []string) {
	if len(written) == 0 {
		return
	}
	for _, path := range written[:len(written)-1] {
		fmt.Println("Informe de cadena:", path)
	}
	fmt.Println("Comparativa:", written[len(written)-1])
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

// metaMatchVersion guarda con qué versión de las reglas se resolvió la lista.
const metaMatchVersion = "match_version"

// reglasCambiadas indica si la base se resolvió con otras reglas de
// coincidencia. En ese caso los matches guardados ya no son de fiar: sin esto,
// un producto equivocado de una versión anterior se queda en la base para
// siempre, porque su puntuación era alta.
func reglasCambiadas(st *store.Store) (bool, error) {
	guardada, err := st.Meta(metaMatchVersion)
	if err != nil {
		return false, err
	}
	return guardada != version.Matching, nil
}

func needsResolve(matches []store.Match, items []list.Item, chains []string) bool {
	cobertos := make(map[string]map[string]bool, len(items))
	historicos := make(map[string]bool)
	for _, m := range matches {
		if m.Score < match.AutoThreshold {
			return true
		}
		if cobertos[m.ItemName] == nil {
			cobertos[m.ItemName] = make(map[string]bool, len(chains))
		}
		cobertos[m.ItemName][m.Chain] = true
		historicos[m.ItemName] = true
	}
	for _, it := range items {
		if len(cobertos[it.Name]) < len(chains) {
			return true
		}
		delete(historicos, it.Name)
	}
	return len(historicos) > 0
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
