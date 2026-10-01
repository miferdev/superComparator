package store

import (
	"database/sql"
	"fmt"
)

// migration es un bloque de sentencias que se aplica una sola vez. El esquema
// antiguo (migrate) no tiene versión, así que la numeración empieza en 1 y
// las migraciones nuevas se añaden al final de migrations.
type migration struct {
	version int
	name    string
	// after se ejecuta dentro de la transacción y antes de las sentencias, para
	// los arreglos sobre tablas que vienen de una migración anterior.
	after func(tx *sql.Tx) error
	stmts []string
}

// backfillHistorySQL empareja el historial del esquema antiguo con el catálogo
// por cadena y URL. Va al final porque necesita que products ya exista.
const backfillHistorySQL = `UPDATE price_history SET product_id = (
	SELECT p.id FROM products p
	WHERE p.chain = price_history.chain AND p.url = price_history.product_url
) WHERE product_id IS NULL`

var migrations = []migration{
	{
		version: 1,
		name:    "catalogo de productos",
		// price_history viene del esquema antiguo: se le añade product_id antes
		// de que exista el catálogo, y se rellena al final de la migración.
		after: func(tx *sql.Tx) error {
			return ensureColumn(tx, "price_history", "product_id", "product_id INTEGER")
		},
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS chains (
  id                TEXT PRIMARY KEY,
  nombre            TEXT NOT NULL,
  sitemap_url       TEXT NOT NULL,
  precios_activos   INTEGER NOT NULL DEFAULT 1,
  pausa_segundos    REAL NOT NULL DEFAULT 2,
  concurrencia      INTEGER NOT NULL DEFAULT 1,
  precio_max_horas  INTEGER NOT NULL DEFAULT 24
)`,
			`CREATE TABLE IF NOT EXISTS products (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  chain             TEXT NOT NULL REFERENCES chains(id),
  url               TEXT NOT NULL,
  sku               TEXT,
  name              TEXT NOT NULL,
  search_name       TEXT NOT NULL,
  format            TEXT,
  measure_value     REAL NOT NULL DEFAULT 0,
  measure_unit      TEXT NOT NULL DEFAULT '',
  category          TEXT,
  name_source       TEXT NOT NULL DEFAULT 'slug',
  price             REAL NOT NULL DEFAULT 0,
  price_basis       TEXT NOT NULL DEFAULT '',
  measure_price     REAL NOT NULL DEFAULT 0,
  available         INTEGER NOT NULL DEFAULT 1,
  price_fetched_at  TEXT,
  crawl_state       TEXT NOT NULL DEFAULT 'catalogado',
  first_seen        TEXT NOT NULL,
  last_seen         TEXT NOT NULL,
  UNIQUE(chain, url)
)`,
			`CREATE INDEX IF NOT EXISTS idx_products_chain ON products(chain)`,
			`CREATE INDEX IF NOT EXISTS idx_products_crawl ON products(crawl_state)`,
			`CREATE INDEX IF NOT EXISTS idx_products_price ON products(price)`,
			`CREATE TABLE IF NOT EXISTS price_queue (
  product_id        INTEGER PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  state             TEXT NOT NULL DEFAULT 'pendiente',
  attempts          INTEGER NOT NULL DEFAULT 0,
  next_attempt_at   TEXT NOT NULL,
  last_error        TEXT
)`,
			`CREATE INDEX IF NOT EXISTS idx_queue_state ON price_queue(state, next_attempt_at)`,
			`CREATE TABLE IF NOT EXISTS list_items (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  product_id        INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  quantity          INTEGER NOT NULL DEFAULT 1,
  position          INTEGER NOT NULL DEFAULT 0,
  added_at          TEXT NOT NULL,
  UNIQUE(product_id)
)`,
			`CREATE TABLE IF NOT EXISTS crawl_state (
  chain             TEXT PRIMARY KEY REFERENCES chains(id),
  phase             TEXT NOT NULL DEFAULT '',
  cursor            TEXT NOT NULL DEFAULT '',
  done              INTEGER NOT NULL DEFAULT 0,
  total             INTEGER NOT NULL DEFAULT 0,
  paused            INTEGER NOT NULL DEFAULT 0,
  updated_at        TEXT NOT NULL
)`,
			`CREATE TABLE IF NOT EXISTS runs (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  kind              TEXT NOT NULL,
  chain             TEXT NOT NULL DEFAULT '',
  started_at        TEXT NOT NULL,
  finished_at       TEXT,
  products          INTEGER NOT NULL DEFAULT 0,
  errors            INTEGER NOT NULL DEFAULT 0,
  note              TEXT
)`,
			// FTS5 normal (no external content) con rowid = products.id: así los
			// triggers pueden mantenerlo sin depender del contenido de la tabla.
			`CREATE VIRTUAL TABLE IF NOT EXISTS products_fts USING fts5(
  name,
  search_name,
  tokenize = "unicode61 remove_diacritics 2"
)`,
			`CREATE TRIGGER IF NOT EXISTS products_fts_ai AFTER INSERT ON products BEGIN
  INSERT INTO products_fts (rowid, name, search_name) VALUES (new.id, new.name, new.search_name);
END`,
			`CREATE TRIGGER IF NOT EXISTS products_fts_ad AFTER DELETE ON products BEGIN
  DELETE FROM products_fts WHERE rowid = old.id;
END`,
			`CREATE TRIGGER IF NOT EXISTS products_fts_au AFTER UPDATE ON products BEGIN
  DELETE FROM products_fts WHERE rowid = old.id;
  INSERT INTO products_fts (rowid, name, search_name) VALUES (new.id, new.name, new.search_name);
END`,
			`INSERT INTO products_fts (rowid, name, search_name)
SELECT p.id, p.name, p.search_name FROM products p
WHERE p.id NOT IN (SELECT rowid FROM products_fts)`,
			backfillHistorySQL,
		},
	},
}

// backfillHistory empareja el historial del esquema antiguo con el catálogo. Lo
// que no tiene ficha se queda sin product_id: se prefiere una fila suelta a
// enlazarla con un producto equivocado.
func backfillHistory(db querier) error {
	_, err := db.Exec(backfillHistorySQL)
	return err
}

// applyMigrations aplica las migraciones pendientes en orden, cada una en su
// propia transacción, y anota en schema_migrations las ya aplicadas.
func (s *Store) applyMigrations() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
)`); err != nil {
		return err
	}
	for _, m := range migrations {
		var applied int
		if err := s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		if err := s.applyMigration(m); err != nil {
			return fmt.Errorf("migración %d (%s): %w", m.version, m.name, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if m.after != nil {
		if err := m.after(tx); err != nil {
			return err
		}
	}
	for _, stmt := range m.stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.version, now()); err != nil {
		return err
	}
	return tx.Commit()
}
