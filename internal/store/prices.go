package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
)

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
