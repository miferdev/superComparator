package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/report"
	"github.com/miferdev/superComparator/internal/version"
)

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
			written, err := report.WriteAll(cfg.ReportPath, cmp)
			if err != nil {
				return err
			}
			fmt.Print(report.Console(cmp))
			fmt.Println()
			printFiles(written)
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
			chains, err := selectChains(cfg)
			if err != nil {
				return err
			}
			defer closeChains(chains)

			urls := map[string]string{
				"mercadona": f.mercadonaU,
				"ahorramas": f.ahorramasU,
				"dia":       f.diaU,
				"alcampo":   f.alcampoU,
			}
			for _, ch := range chains {
				url := urls[ch.ID()]
				if url == "" {
					fmt.Printf("%s: sin url de prueba\n", report.ChainName(ch.ID()))
					continue
				}
				p, err := ch.Fetch(ctx, url)
				if err != nil {
					fmt.Printf("%s: %s\n", report.ChainName(ch.ID()), err)
					continue
				}
				out, _ := json.MarshalIndent(p, "", "  ")
				fmt.Printf("%s\n%s\n", report.ChainName(ch.ID()), out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.mercadonaU, "mercadona-url", "https://tienda.mercadona.es/product/10005/chocolate-liquido-taza-hacendado-brick", "url de prueba de Mercadona")
	cmd.Flags().StringVar(&f.ahorramasU, "ahorramas-url", "https://www.ahorramas.com/garbanzo-cocido-luengo-400g-44569.html", "url de prueba de Ahorramas")
	cmd.Flags().StringVar(&f.diaU, "dia-url", "https://www.dia.es/huevos-leche-y-mantequilla/leche/p/16065", "url de prueba de DÍA")
	cmd.Flags().StringVar(&f.alcampoU, "alcampo-url", "https://www.compraonline.alcampo.es/products/pan-de-trigo-de-espelta-64-400g/56004", "url de prueba de Alcampo")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println(version.Stamp()) },
	}
}

// needsResolve indica si hay que resolver de nuevo. Se resuelve si falta
// algún producto de la lista, si algún match quedó con confianza baja, si
// sobra algún producto del histórico o si a algún producto le falta el match
// de una cadena activa (por ejemplo al añadir una cadena nueva).

// explainCmd enseña por qué se eligió cada producto: los candidatos que hay en
// cada cadena, su puntuación y el motivo de cada rechazo. No toca la base de
// datos.
func explainCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Enseña por qué se eligió cada producto (diagnóstico)",
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := loadConfig(f)
			items, err := list.ParseFile(cfg.ListaPath)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				items = filterItems(items, args)
			}
			if len(items) == 0 {
				return fmt.Errorf("no hay productos que explicar en %s", cfg.ListaPath)
			}
			c, cleanup, err := build(cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			fmt.Println("Buscando candidatos… (esto no guarda nada)")
			explicaciones, err := c.Explain(context.Background(), items)
			if err != nil {
				return err
			}
			path := filepath.Join(filepath.Dir(cfg.ReportPath), "explicacion.md")
			if err := report.WriteExplain(path, explicaciones); err != nil {
				return err
			}
			fmt.Print(report.ExplainConsole(explicaciones))
			fmt.Println("\nExplicación completa:", path)
			return nil
		},
	}
	return cmd
}

// filterItems se queda con los productos de la lista cuyo nombre coincide con
// alguno de los argumentos.
func filterItems(items []list.Item, wanted []string) []list.Item {
	var out []list.Item
	for _, it := range items {
		for _, w := range wanted {
			if strings.Contains(strings.ToLower(it.Name), strings.ToLower(w)) {
				out = append(out, it)
				break
			}
		}
	}
	return out
}
