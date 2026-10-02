package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/miferdev/superComparator/internal/catalog"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/server"
	"github.com/miferdev/superComparator/internal/store"
	"github.com/miferdev/superComparator/internal/version"
)

// crawlCmd agrupa las tareas de mantenimiento del catálogo.
func crawlCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "crawl",
		Short:   "Tareas del catálogo",
		Aliases: []string{"catalogo"},
	}
	cmd.AddCommand(indexCmd(f))
	cmd.AddCommand(preciosCmd(f))
	return cmd
}

// indexCmd lee los sitemaps y guarda el catálogo en la base de datos. Es la
// parte rápida: no descarga fichas, solo nombres, medidas y enlaces.
func indexCmd(f *flags) *cobra.Command {
	var max int
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Indexa los catálogos desde los sitemaps",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadConfig(f)
			chains, err := selectChains(cfg, cfg.Chains)
			if err != nil {
				return err
			}
			defer closeChains(chains)

			st, err := store.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := seedCatalog(st); err != nil {
				return err
			}

			log := loggerFor(cfg)
			indexer := catalog.NewIndexer(chains, st, log).WithOptions(catalog.IndexerOptions{
				MaxProducts: max,
				OnProgress:  func(chainID string, hechos, total int) { fmt.Printf("  %-10s %d/%d\n", chainID, hechos, total) },
			})
			fmt.Printf("Indexando %d cadenas…\n", len(chains))
			if err := indexer.IndexCatalog(context.Background()); err != nil {
				return err
			}
			printCounts(st)
			return nil
		},
	}
	cmd.Flags().IntVar(&max, "max", 0, "máximo de productos por cadena (0 = todos); útil para probar")
	return cmd
}

// preciosCmd descarga fichas de la cola y guarda sus precios. Es la parte lenta:
// respeta el ritmo de cada tienda, así que con --limit se puede probar sin
// esperar horas.
func preciosCmd(f *flags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "precios",
		Short: "Descarga fichas de la cola y guarda los precios",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadConfig(f)
			chains, err := selectChains(cfg, cfg.Chains)
			if err != nil {
				return err
			}
			defer closeChains(chains)

			st, err := store.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := seedCatalog(st); err != nil {
				return err
			}

			cat := catalog.New(st, loggerFor(cfg))
			ctx := context.Background()
			fmt.Printf("Descargando precios de %d cadenas…\n", len(chains))
			res, err := catalog.NewPricesJob(cat, chains).RunLimited(ctx, limit)
			fmt.Printf("  %d fichas con precio, %d fallidas, en %s\n",
				res.Hechos, res.Fallidos, res.Duracion.Round(time.Second))
			printCounts(st)
			return err
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "máximo de fichas en esta pasada (0 = la cola entera)")
	return cmd
}

// printCounts resume lo que ha quedado guardado.
func printCounts(st *store.Store) {
	counts, err := st.CatalogCounts()
	if err != nil {
		fmt.Println("no se pudo leer el recuento:", err)
		return
	}
	if len(counts) == 0 {
		fmt.Println("El catálogo está vacío.")
		return
	}
	fmt.Println("\nCadena        En catálogo   Con precio   Sin precio")
	for _, n := range counts {
		fmt.Printf("%-12s  %10d  %11d  %11d\n", n.Chain, n.Total, n.ConPrecio, n.SinPrecio)
	}
}

// smokeCmd descarga una ficha de cada cadena para diagnóstico.
func smokeCmd(f *flags) *cobra.Command {
	var urls map[string]string
	cmd := &cobra.Command{
		Use:   "smoke",
		Short: "Descarga una ficha de cada cadena (diagnóstico)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadConfig(f)
			chains, err := selectChains(cfg, cfg.Chains)
			if err != nil {
				return err
			}
			defer closeChains(chains)

			for _, ch := range chains {
				url := urls[ch.ID()]
				if url == "" {
					fmt.Printf("%s: sin url de prueba\n", ch.ID())
					continue
				}
				p, err := ch.Fetch(context.Background(), url)
				if err != nil {
					fmt.Printf("%-10s %s\n", ch.ID(), err)
					continue
				}
				out, _ := json.MarshalIndent(p, "", "  ")
				fmt.Printf("%-10s %s\n", ch.ID(), out)
			}
			return nil
		},
	}
	cmd.Flags().StringToStringVar(&urls, "url", nil, "url de prueba por cadena, p. ej. --url dia=https://…")
	return cmd
}

func versionStamp() string { return version.Stamp() }

func newServer(cat *catalog.Catalog, st *store.Store, log *slog.Logger, cfg config.Config) *server.Server {
	return server.New(cat, st, log, cfg.Addr)
}
