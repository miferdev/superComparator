// Package config carga la configuración desde variables de entorno.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultAddr hace que la web solo se pueda abrir desde este ordenador.
	DefaultAddr = "127.0.0.1:8080"
	// DefaultPostalCode es el código postal con el que se fija la tienda de
	// Mercadona.
	DefaultPostalCode = "28032"
)

// Config es todo lo que el programa necesita saber de su entorno.
type Config struct {
	// Addr es la dirección donde escucha el servidor.
	Addr       string
	DBPath     string
	Chains     []string
	PostalCode string
	BrowserBin string
	Timeout    time.Duration
	LogPath    string
}

func Load() Config {
	c := Config{
		Addr:       env("SUPERCOMPARATOR_ADDR", DefaultAddr),
		DBPath:     env("SUPERCOMPARATOR_DB", filepath.Join("datos", "catalogo.db")),
		PostalCode: env("SUPERCOMPARATOR_CP", DefaultPostalCode),
		BrowserBin: env("SUPERCOMPARATOR_BROWSER_BIN", os.Getenv("ROD_BROWSER_BIN")),
		Timeout:    time.Duration(envInt("SUPERCOMPARATOR_TIMEOUT_S", 40)) * time.Second,
		LogPath:    env("SUPERCOMPARATOR_LOG", ""),
	}
	c.Chains = SplitChains(env("SUPERCOMPARATOR_CADENAS", ""))
	return c
}

// SplitChains convierte "mercadona,día" en ["mercadona", "dia"]. Acepta
// separadores de coma o espacio.
func SplitChains(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

// EnsureDirs crea el directorio de la base de datos.
func (c Config) EnsureDirs() error {
	if err := os.MkdirAll(filepath.Dir(c.DBPath), 0o755); err != nil {
		return err
	}
	if c.LogPath != "" {
		return os.MkdirAll(filepath.Dir(c.LogPath), 0o755)
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n := atoi(v); n > 0 {
			return n
		}
	}
	return def
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
