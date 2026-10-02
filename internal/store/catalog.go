package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Chain es una tienda del catálogo: de dónde se saca el sitemap y con qué
// ritmo se permite golpearla.
type Chain struct {
	ID             string
	Nombre         string
	SitemapURL     string
	PreciosActivos bool
	PausaSegundos  float64
	Concurrencia   int
	PrecioMaxHoras int
}

// Product es una ficha del catálogo. Price y MeasurePrice los rellena
// SetPrice al descargar la ficha; el reindexado del sitemap no los toca.
type Product struct {
	ID             int64
	Chain          string
	URL            string
	SKU            string
	Name           string
	SearchName     string
	Format         string
	MeasureValue   float64
	MeasureUnit    string
	Category       string
	NameSource     string
	Price          float64
	PriceBasis     string
	MeasurePrice   float64
	Available      bool
	PriceFetchedAt time.Time
	CrawlState     string
	FirstSeen      time.Time
	LastSeen       time.Time
}

// CatalogCount resume el tamaño del catálogo de cada cadena.
type CatalogCount struct {
	Chain     string
	Total     int
	ConPrecio int
	SinPrecio int
}

const productColumns = `p.id, p.chain, p.url, COALESCE(p.sku, ''), p.name, p.search_name,
	COALESCE(p.format, ''), p.measure_value, p.measure_unit, COALESCE(p.category, ''),
	p.name_source, p.price, p.price_basis, p.measure_price, p.available,
	COALESCE(p.price_fetched_at, ''), p.crawl_state, p.first_seen, p.last_seen`

func scanProduct(sc interface{ Scan(dest ...any) error }) (Product, error) {
	var p Product
	var available int
	var priceFetchedAt, firstSeen, lastSeen string
	if err := sc.Scan(&p.ID, &p.Chain, &p.URL, &p.SKU, &p.Name, &p.SearchName, &p.Format,
		&p.MeasureValue, &p.MeasureUnit, &p.Category, &p.NameSource, &p.Price, &p.PriceBasis,
		&p.MeasurePrice, &available, &priceFetchedAt, &p.CrawlState, &firstSeen, &lastSeen); err != nil {
		return Product{}, err
	}
	p.Available = available == 1
	p.PriceFetchedAt = parseTime(priceFetchedAt)
	p.FirstSeen = parseTime(firstSeen)
	p.LastSeen = parseTime(lastSeen)
	return p, nil
}

func scanChains(rows *sql.Rows) ([]Chain, error) {
	var out []Chain
	for rows.Next() {
		var c Chain
		var preciosActivos int
		if err := rows.Scan(&c.ID, &c.Nombre, &c.SitemapURL, &preciosActivos,
			&c.PausaSegundos, &c.Concurrencia, &c.PrecioMaxHoras); err != nil {
			return nil, err
		}
		c.PreciosActivos = preciosActivos == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// SeedChains da de alta las cadenas que falten. Si la cadena ya existe solo
// rellena nombre y sitemap_url vacíos: precios_activos y pausa_segundos son
// ajustes del usuario y un arranque no debe pisarlos.
func (s *Store) SeedChains(chains []Chain) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range chains {
		flag := 0
		if c.PreciosActivos {
			flag = 1
		}
		if _, err := tx.Exec(`
			INSERT INTO chains (id, nombre, sitemap_url, precios_activos, pausa_segundos, concurrencia, precio_max_horas)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				nombre = CASE WHEN chains.nombre = '' THEN excluded.nombre ELSE chains.nombre END,
				sitemap_url = CASE WHEN chains.sitemap_url = '' THEN excluded.sitemap_url ELSE chains.sitemap_url END`,
			c.ID, c.Nombre, c.SitemapURL, flag, c.PausaSegundos, c.Concurrencia, c.PrecioMaxHoras); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Chains devuelve las cadenas del catálogo ordenadas por identificador.
func (s *Store) Chains() ([]Chain, error) {
	rows, err := s.db.Query(`
		SELECT id, nombre, sitemap_url, precios_activos, pausa_segundos, concurrencia, precio_max_horas
		FROM chains ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChains(rows)
}

// Chain devuelve una cadena por su identificador.
func (s *Store) Chain(id string) (Chain, bool, error) {
	row := s.db.QueryRow(`
		SELECT id, nombre, sitemap_url, precios_activos, pausa_segundos, concurrencia, precio_max_horas
		FROM chains WHERE id = ?`, id)
	var c Chain
	var preciosActivos int
	if err := row.Scan(&c.ID, &c.Nombre, &c.SitemapURL, &preciosActivos,
		&c.PausaSegundos, &c.Concurrencia, &c.PrecioMaxHoras); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Chain{}, false, nil
		}
		return Chain{}, false, err
	}
	c.PreciosActivos = preciosActivos == 1
	return c, true, nil
}

// UpsertProducts cataloga un lote de fichas de una cadena en una sola
// transacción. Reindexar el sitemap actualiza los datos de la ficha pero deja
// como estaban el precio y su marca, que son cosa de SetPrice. Devuelve
// cuántos productos eran nuevos y cuántos se actualizaron.
func (s *Store) UpsertProducts(chain string, ps []Product) (inserted int, updated int, err error) {
	if len(ps) == 0 {
		return 0, 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	stamp := ts(time.Now())
	for _, p := range ps {
		searchName := p.SearchName
		if searchName == "" {
			searchName = p.Name
		}
		nameSource := p.NameSource
		if nameSource == "" {
			nameSource = "slug"
		}
		crawlState := p.CrawlState
		if crawlState == "" {
			crawlState = "catalogado"
		}
		var exists int
		if err := tx.QueryRow(`SELECT count(*) FROM products WHERE chain = ? AND url = ?`,
			chain, p.URL).Scan(&exists); err != nil {
			return 0, 0, err
		}
		res, err := tx.Exec(`
			INSERT INTO products
				(chain, url, sku, name, search_name, format, measure_value, measure_unit, category,
				 name_source, crawl_state, first_seen, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(chain, url) DO UPDATE SET
				name = excluded.name,
				search_name = excluded.search_name,
				sku = excluded.sku,
				format = excluded.format,
				measure_value = excluded.measure_value,
				measure_unit = excluded.measure_unit,
				category = excluded.category,
				name_source = excluded.name_source,
				crawl_state = excluded.crawl_state,
				last_seen = excluded.last_seen`,
			chain, p.URL, p.SKU, p.Name, searchName, p.Format, p.MeasureValue, p.MeasureUnit,
			p.Category, nameSource, crawlState, stamp, stamp)
		if err != nil {
			return 0, 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, err
		}
		if n == 0 {
			continue
		}
		if exists > 0 {
			updated++
		} else {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, updated, nil
}

// Product devuelve la ficha de una cadena por su URL.
func (s *Store) Product(chain, url string) (Product, bool, error) {
	row := s.db.QueryRow(`SELECT `+productColumns+` FROM products p WHERE p.chain = ? AND p.url = ?`,
		chain, url)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Product{}, false, nil
		}
		return Product{}, false, err
	}
	return p, true, nil
}

// ProductByID devuelve la ficha por su identificador interno.
func (s *Store) ProductByID(id int64) (Product, bool, error) {
	row := s.db.QueryRow(`SELECT `+productColumns+` FROM products p WHERE p.id = ?`, id)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Product{}, false, nil
		}
		return Product{}, false, err
	}
	return p, true, nil
}

// CatalogCounts cuenta los productos de cada cadena y cómo se reparten entre
// los que tienen precio y los que aún no lo han descargado.
func (s *Store) CatalogCounts() ([]CatalogCount, error) {
	rows, err := s.db.Query(`
		SELECT c.id, count(p.id),
		       COALESCE(sum(CASE WHEN p.id IS NULL OR p.price = 0 THEN 0 ELSE 1 END), 0),
		       COALESCE(sum(CASE WHEN p.id IS NOT NULL AND p.price = 0 THEN 1 ELSE 0 END), 0)
		FROM chains c LEFT JOIN products p ON p.chain = c.id
		GROUP BY c.id ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogCount
	for rows.Next() {
		var c CatalogCount
		if err := rows.Scan(&c.Chain, &c.Total, &c.ConPrecio, &c.SinPrecio); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FichaData escribe el nombre y la categoría definitivos de un producto, los que
// publica su propia ficha y no su sitemap. El nombre solo se toca si viene
// informado: un producto con buen nombre de catálogo no debe degradarse a
// «categoria» porque la ficha no trajera nombre.
func (s *Store) FichaData(productID int64, name, searchName, category string) (string, string, error) {
	var nombreActual, categoriaActual string
	err := s.db.QueryRow(`SELECT name, COALESCE(category, '') FROM products WHERE id = ?`, productID).
		Scan(&nombreActual, &categoriaActual)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("producto %d no está en el catálogo", productID)
	}
	if err != nil {
		return "", "", err
	}

	nombre, categoria := nombreActual, categoriaActual
	if n := strings.TrimSpace(name); n != "" {
		nombre = n
	}
	if cat := strings.TrimSpace(category); cat != "" {
		categoria = cat
	}
	if nombre == nombreActual && categoria == categoriaActual {
		return nombre, categoria, nil
	}

	// search_name solo se reescribe si viene informado: si la ficha no trae
	// nombre, dejarlo como estaba es mejor que sustituir los tokens que ya
	// tenía por el nombre en crudo.
	//
	// Con el nombre de la ficha el producto deja de estar pendiente: DÍÁ los
	// tenía marcados así porque su sitemap solo traía la categoría.
	if _, err := s.db.Exec(`
		UPDATE products
		SET name = ?,
		    search_name = CASE WHEN ? <> '' THEN ? ELSE search_name END,
		    category = ?,
		    name_source = CASE WHEN ? <> '' THEN 'ficha' ELSE name_source END,
		    crawl_state = CASE WHEN ? <> '' THEN 'catalogado' ELSE crawl_state END
		WHERE id = ?`, nombre, searchName, searchName, categoria,
		strings.TrimSpace(name), strings.TrimSpace(name), productID); err != nil {
		return "", "", err
	}
	return nombre, categoria, nil
}

// ProductsWithPrice cuenta los productos de una cadena que ya tienen precio, para
// saber cuánto del catálogo está cubierto. Los que solo tienen precio por medida
// también cuentan: es un precio publicado, no un dato inventado.
func (s *Store) ProductsWithPrice(chainID string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT count(*) FROM products
		WHERE chain = ? AND (price > 0 OR measure_price > 0)`, chainID).Scan(&n)
	return n, err
}
