// Package config carga la configuración desde variables de entorno.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const DefaultPostalCode = "28032"

type Config struct {
	PostalCode string
	ListaPath  string
	ListaDir   string
	DBPath     string
	ReportPath string
	Workers    int
	Delay      time.Duration
	Candidates int
	Timeout    time.Duration
	BrowserBin string
	LogPath    string
}

func Load() Config {
	db := env("SUPERCOMPARATOR_DB", filepath.Join("datos", "precios.db"))
	c := Config{
		PostalCode: env("SUPERCOMPARATOR_CP", DefaultPostalCode),
		ListaPath:  env("SUPERCOMPARATOR_LISTA", ""),
		ListaDir:   env("SUPERCOMPARATOR_LISTA_DIR", ""),
		DBPath:     db,
		ReportPath: env("SUPERCOMPARATOR_REPORT", filepath.Join(filepath.Dir(db), "informe.md")),
		Workers:    envInt("SUPERCOMPARATOR_WORKERS", 3),
		Delay:      time.Duration(envInt("SUPERCOMPARATOR_DELAY_MS", 300)) * time.Millisecond,
		Candidates: envInt("SUPERCOMPARATOR_CANDIDATES", 3),
		Timeout:    time.Duration(envInt("SUPERCOMPARATOR_TIMEOUT_S", 40)) * time.Second,
		BrowserBin: env("SUPERCOMPARATOR_BROWSER_BIN", os.Getenv("ROD_BROWSER_BIN")),
		LogPath:    env("SUPERCOMPARATOR_LOG", ""),
	}
	if c.ListaDir == "" {
		if _, err := os.Stat("/compras"); err == nil {
			c.ListaDir = "/compras"
		} else {
			c.ListaDir = "."
		}
	}
	return c
}

func (c Config) EnsureDirs() error {
	if err := os.MkdirAll(filepath.Dir(c.DBPath), 0o755); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(c.ReportPath), 0o755)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
