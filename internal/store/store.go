// Package store guarda el catálogo de productos y el historial de precios en
// SQLite (driver puro Go, sin cgo).
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.applyMigrations(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate crea el historial de precios, que ya existía antes de las migraciones
// versionadas y por eso sigue aquí: guardarlo es lo que permite ver de cuánto es
// un precio. Si viene de una versión antigua, la migración 1 le añade la columna
// product_id y la rellena.
func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS price_history (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	chain         TEXT NOT NULL,
	product_url   TEXT NOT NULL,
	sku           TEXT,
	name          TEXT,
	brand         TEXT,
	format        TEXT,
	price         REAL NOT NULL,
	unit_price    REAL,
	measure_price REAL,
	measure_unit  TEXT,
	old_price     REAL,
	promo_text    TEXT,
	available     INTEGER NOT NULL DEFAULT 1,
	fetched_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_history_url ON price_history(chain, product_url, fetched_at DESC);
`
	_, err := s.db.Exec(schema)
	return err
}

// Item es una línea de la lista tal como está guardada.
func now() string { return time.Now().Format(time.RFC3339) }

// ts formatea un instante en UTC. Las marcas del catálogo se guardan y se
// comparan como texto, así que todas tienen que llevar la misma zona.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseTime lee una marca del catálogo. Si no hay marca o no se entiende,
// devuelve el instante cero en vez de fallar.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// DB expone la conexión para lo que necesite tocar el esquema a medida (por
// ejemplo, tests que ajustan la configuración de una cadena).
func (s *Store) DB() *sql.DB { return s.db }
