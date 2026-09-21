package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/list"
)

func TestRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ids, err := st.SyncItems([]list.Item{
		{Name: "Leche entera 1L", Quantity: 3},
		{Name: "Pan de molde", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("SyncItems: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %+v", ids)
	}

	p := chain.Product{
		Chain: "mercadona", URL: "https://tienda.mercadona.es/product/1/leche",
		SKU: "1", Name: "Leche entera Hacendado Brick 1 L", Price: 2.45,
		MeasurePrice: 2.45, MeasureUnit: "l", Available: true, FetchedAt: time.Now(),
	}
	if err := st.SetMatch(ids["Leche entera 1L"], "mercadona", p, 0.9); err != nil {
		t.Fatalf("SetMatch: %v", err)
	}
	if err := st.InsertPrice("mercadona", p); err != nil {
		t.Fatalf("InsertPrice: %v", err)
	}

	matches, err := st.Matches()
	if err != nil || len(matches) != 1 {
		t.Fatalf("Matches = %+v, %v", matches, err)
	}
	if matches[0].ItemName != "Leche entera 1L" || matches[0].Quantity != 3 {
		t.Fatalf("match = %+v", matches[0])
	}

	latest, ok, err := st.LatestPrice("mercadona", p.URL)
	if err != nil || !ok || latest.Price != 2.45 {
		t.Fatalf("LatestPrice = %+v, %v, %v", latest, ok, err)
	}

	p.Price = 2.60
	p.FetchedAt = time.Now().Add(time.Second)
	if err := st.InsertPrice("mercadona", p); err != nil {
		t.Fatalf("InsertPrice: %v", err)
	}
	changes, err := st.Changes(10)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 || changes[0].OldPrice != 2.45 || changes[0].NewPrice != 2.60 {
		t.Fatalf("changes = %+v", changes)
	}

	rows, err := st.LastMatchPrices()
	if err != nil || len(rows) != 1 || rows[0].Price != 2.60 {
		t.Fatalf("LastMatchPrices = %+v, %v", rows, err)
	}
}

func TestOpenMigratesOldMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE matches (
		item_id      INTEGER NOT NULL,
		chain        TEXT NOT NULL,
		product_url  TEXT NOT NULL,
		sku          TEXT,
		matched_name TEXT,
		score        REAL NOT NULL DEFAULT 0,
		updated_at   TEXT NOT NULL,
		PRIMARY KEY (item_id, chain));`); err != nil {
		t.Fatalf("esquema antiguo: %v", err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open migrando: %v", err)
	}
	defer st.Close()
	if err := st.SetMatchAvailable(1, "mercadona", false); err != nil {
		t.Fatalf("la columna available no se migró: %v", err)
	}
}

func TestSyncRemovesOldItems(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, err := st.SyncItems([]list.Item{{Name: "A", Quantity: 1}, {Name: "B", Quantity: 1}}); err != nil {
		t.Fatalf("SyncItems: %v", err)
	}
	if _, err := st.SyncItems([]list.Item{{Name: "A", Quantity: 2}}); err != nil {
		t.Fatalf("SyncItems: %v", err)
	}
	matches, err := st.Matches()
	if err != nil {
		t.Fatalf("Matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %+v", matches)
	}
}
