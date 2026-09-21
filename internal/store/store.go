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

type Match struct {
	ItemID      int64
	ItemName    string
	Quantity    int
	Chain       string
	URL         string
	SKU         string
	MatchedName string
	Score       float64
	Available   bool
}

type Price struct {
	Chain        string
	URL          string
	SKU          string
	Name         string
	Brand        string
	Format       string
	Price        float64
	UnitPrice    float64
	MeasurePrice float64
	MeasureUnit  string
	OldPrice     float64
	PromoText    string
	Available    bool
	FetchedAt    time.Time
}

type MatchedPrice struct {
	ItemID       int64
	ItemName     string
	Quantity     int
	Chain        string
	URL          string
	SKU          string
	MatchedName  string
	Score        float64
	Format       string
	Price        float64
	MeasurePrice float64
	MeasureUnit  string
	OldPrice     float64
	Available    bool
	FetchedAt    time.Time
}

type Change struct {
	Chain     string
	URL       string
	Name      string
	OldPrice  float64
	NewPrice  float64
	FetchedAt time.Time
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

func (s *Store) InsertPrice(chainID string, p chain.Product) error {
	_, err := s.db.Exec(`
		INSERT INTO price_history
			(chain, product_url, sku, name, brand, format, price, unit_price, measure_price, measure_unit, old_price, promo_text, available, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chainID, p.URL, p.SKU, p.Name, p.Brand, p.Format, p.Price, p.UnitPrice,
		p.MeasurePrice, p.MeasureUnit, p.OldPrice, p.PromoText, p.Available, p.FetchedAt.Format(time.RFC3339))
	return err
}

func (s *Store) LatestPrice(chainID, url string) (Price, bool, error) {
	row := s.db.QueryRow(`
		SELECT chain, product_url, COALESCE(sku, ''), COALESCE(name, ''), COALESCE(brand, ''), COALESCE(format, ''),
		       price, COALESCE(unit_price, 0), COALESCE(measure_price, 0), COALESCE(measure_unit, ''),
		       COALESCE(old_price, 0), COALESCE(promo_text, ''), available, fetched_at
		FROM price_history
		WHERE chain = ? AND product_url = ?
		ORDER BY fetched_at DESC, id DESC
		LIMIT 1`, chainID, url)
	var p Price
	var fetched string
	var available int
	if err := row.Scan(&p.Chain, &p.URL, &p.SKU, &p.Name, &p.Brand, &p.Format, &p.Price,
		&p.UnitPrice, &p.MeasurePrice, &p.MeasureUnit, &p.OldPrice, &p.PromoText, &available, &fetched); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Price{}, false, nil
		}
		return Price{}, false, err
	}
	p.Available = available == 1
	p.FetchedAt, _ = time.Parse(time.RFC3339, fetched)
	return p, true, nil
}

// LastMatchPrices devuelve cada match con su último precio conocido.
func (s *Store) LastMatchPrices() ([]MatchedPrice, error) {
	rows, err := s.db.Query(`
		SELECT i.id, i.name, i.quantity, m.chain, m.product_url, COALESCE(m.sku, ''), COALESCE(m.matched_name, ''), m.score,
		       COALESCE(m.available, 1),
		       COALESCE(h.price, 0), COALESCE(h.measure_price, 0), COALESCE(h.measure_unit, ''), COALESCE(h.old_price, 0),
		       COALESCE(h.available, 0), COALESCE(h.format, ''), COALESCE(h.fetched_at, '')
		FROM matches m
		JOIN items i ON i.id = m.item_id
		LEFT JOIN price_history h ON h.id = (
			SELECT ph.id FROM price_history ph
			WHERE ph.chain = m.chain AND ph.product_url = m.product_url
			ORDER BY ph.fetched_at DESC, ph.id DESC LIMIT 1
		)
		ORDER BY i.position, m.chain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchedPrice
	for rows.Next() {
		var mp MatchedPrice
		var matchAvailable, historyAvailable int
		var fetched string
		if err := rows.Scan(&mp.ItemID, &mp.ItemName, &mp.Quantity, &mp.Chain, &mp.URL, &mp.SKU, &mp.MatchedName, &mp.Score,
			&matchAvailable, &mp.Price, &mp.MeasurePrice, &mp.MeasureUnit, &mp.OldPrice, &historyAvailable, &mp.Format, &fetched); err != nil {
			return nil, err
		}
		mp.Available = matchAvailable == 1 && historyAvailable == 1
		mp.FetchedAt, _ = time.Parse(time.RFC3339, fetched)
		out = append(out, mp)
	}
	return out, rows.Err()
}

// Changes lista los cambios de precio registrados, del más reciente al más antiguo.
func (s *Store) Changes(limit int) ([]Change, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT chain, product_url, COALESCE(name, ''), prev_price, price, fetched_at FROM (
			SELECT chain, product_url, name, price,
			       LAG(price) OVER (PARTITION BY chain, product_url ORDER BY fetched_at) AS prev_price,
			       fetched_at
			FROM price_history
		)
		WHERE prev_price IS NOT NULL AND prev_price != price
		ORDER BY fetched_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		var fetched string
		if err := rows.Scan(&c.Chain, &c.URL, &c.Name, &c.OldPrice, &c.NewPrice, &fetched); err != nil {
			return nil, err
		}
		c.FetchedAt, _ = time.Parse(time.RFC3339, fetched)
		out = append(out, c)
	}
	return out, rows.Err()
}

func now() string { return time.Now().Format(time.RFC3339) }
