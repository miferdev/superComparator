// Package store persiste la lista, los productos elegidos y el historial de
// precios en SQLite (driver puro Go, sin cgo).
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/list"
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
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS items (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	name     TEXT NOT NULL UNIQUE,
	quantity INTEGER NOT NULL DEFAULT 1,
	position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS matches (
	item_id      INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	chain        TEXT NOT NULL,
	product_url  TEXT NOT NULL,
	sku          TEXT,
	matched_name TEXT,
	score        REAL NOT NULL DEFAULT 0,
	available    INTEGER NOT NULL DEFAULT 1,
	updated_at   TEXT NOT NULL,
	PRIMARY KEY (item_id, chain)
);
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
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS alternatives (
	item_id       INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	chain         TEXT NOT NULL,
	product_url   TEXT NOT NULL,
	name          TEXT,
	score         REAL NOT NULL DEFAULT 0,
	price         REAL,
	measure_price REAL,
	measure_unit  TEXT,
	updated_at    TEXT NOT NULL,
	PRIMARY KEY (item_id, chain, product_url)
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	return ensureColumn(s.db, "matches", "available", "available INTEGER NOT NULL DEFAULT 1")
}

// SyncItems sincroniza la lista del fichero con la base de datos y devuelve
// el id de cada producto por nombre.
func (s *Store) SyncItems(items []list.Item) (map[string]int64, error) {
	ids := make(map[string]int64, len(items))
	names := make([]any, 0, len(items))
	for pos, it := range items {
		if _, err := s.db.Exec(`
			INSERT INTO items (name, quantity, position) VALUES (?, ?, ?)
			ON CONFLICT(name) DO UPDATE SET quantity = excluded.quantity, position = excluded.position`,
			it.Name, it.Quantity, pos); err != nil {
			return nil, err
		}
		var id int64
		if err := s.db.QueryRow(`SELECT id FROM items WHERE name = ?`, it.Name).Scan(&id); err != nil {
			return nil, err
		}
		ids[it.Name] = id
		names = append(names, it.Name)
	}
	if len(names) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
		args := append([]any{""}, names...)
		args[0] = "DELETE FROM items WHERE name NOT IN (" + placeholders + ")"
		if _, err := s.db.Exec(args[0].(string), args[1:]...); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// Meta guarda datos sueltos sobre el estado de la base, como la versión de las
// reglas con las que se resolvió la lista por última vez.
func (s *Store) Meta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetMeta guarda un valor de estado de la base.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Item es una línea de la lista tal como está guardada.
type Item struct {
	Name     string
	Quantity int
}

// Items devuelve la lista sincronizada, en el orden de lista.md.
func (s *Store) Items() ([]Item, error) {
	rows, err := s.db.Query(`SELECT name, quantity FROM items ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.Name, &it.Quantity); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) SetMatch(itemID int64, chainID string, p chain.Product, score float64) error {
	_, err := s.db.Exec(`
		INSERT INTO matches (item_id, chain, product_url, sku, matched_name, score, available, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT(item_id, chain) DO UPDATE SET
			product_url = excluded.product_url,
			sku = excluded.sku,
			matched_name = excluded.matched_name,
			score = excluded.score,
			available = 1,
			updated_at = excluded.updated_at`,
		itemID, chainID, p.URL, p.SKU, p.Name, score, now())
	return err
}

// SetMatchAvailable marca si el producto vinculado sigue disponible tras un
// chequeo, sin cambiar la URL ni la puntuación del match.
func (s *Store) SetMatchAvailable(itemID int64, chainID string, available bool) error {
	flag := 0
	if available {
		flag = 1
	}
	_, err := s.db.Exec(`
		UPDATE matches SET available = ?, updated_at = ? WHERE item_id = ? AND chain = ?`,
		flag, now(), itemID, chainID)
	return err
}

// ReplaceAlternatives sustituye los candidatos descartados de un ítem en una
// cadena, para poder proponerlos cuando la coincidencia no es concluyente.
func (s *Store) ReplaceAlternatives(itemID int64, chainID string, alts []chain.Alternative) error {
	if _, err := s.db.Exec(`DELETE FROM alternatives WHERE item_id = ? AND chain = ?`, itemID, chainID); err != nil {
		return err
	}
	for _, a := range alts {
		if _, err := s.db.Exec(`
			INSERT INTO alternatives
				(item_id, chain, product_url, name, score, price, measure_price, measure_unit, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			itemID, chainID, a.URL, a.Name, a.Score, a.Price, a.MeasurePrice, a.MeasureUnit, now()); err != nil {
			return err
		}
	}
	return nil
}

// Alternatives devuelve los candidatos guardados, indexados por producto y
// cadena y ordenados de mayor a menor coincidencia.
func (s *Store) Alternatives() (map[string]map[string][]chain.Alternative, error) {
	rows, err := s.db.Query(`
		SELECT i.name, a.chain, a.product_url, COALESCE(a.name, ''), a.score,
		       COALESCE(a.price, 0), COALESCE(a.measure_price, 0), COALESCE(a.measure_unit, '')
		FROM alternatives a
		JOIN items i ON i.id = a.item_id
		ORDER BY i.position, a.chain, a.score DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]map[string][]chain.Alternative)
	for rows.Next() {
		var itemName, chainID string
		var a chain.Alternative
		if err := rows.Scan(&itemName, &chainID, &a.URL, &a.Name, &a.Score,
			&a.Price, &a.MeasurePrice, &a.MeasureUnit); err != nil {
			return nil, err
		}
		if out[itemName] == nil {
			out[itemName] = make(map[string][]chain.Alternative)
		}
		out[itemName][chainID] = append(out[itemName][chainID], a)
	}
	return out, rows.Err()
}

func (s *Store) Matches() ([]Match, error) {
	rows, err := s.db.Query(`
		SELECT i.id, i.name, i.quantity, m.chain, m.product_url, COALESCE(m.sku, ''), COALESCE(m.matched_name, ''), m.score,
		       COALESCE(m.available, 1)
		FROM matches m
		JOIN items i ON i.id = m.item_id
		ORDER BY i.position, m.chain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Match
	for rows.Next() {
		var m Match
		var available int
		if err := rows.Scan(&m.ItemID, &m.ItemName, &m.Quantity, &m.Chain, &m.URL, &m.SKU, &m.MatchedName, &m.Score, &available); err != nil {
			return nil, err
		}
		m.Available = available == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func now() string { return time.Now().Format(time.RFC3339) }
