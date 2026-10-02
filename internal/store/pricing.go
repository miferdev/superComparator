package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
)

// SetPrice guarda el precio descargado de una ficha: actualiza el producto y
// deja la muestra en el historial, todo en la misma transacción para que no
// haya precios sin rastro.
func (s *Store) SetPrice(productID int64, price float64, basis string, measurePrice float64, measureUnit string, available bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var chain, url, name string
	if err := tx.QueryRow(`SELECT chain, url, name FROM products WHERE id = ?`, productID).
		Scan(&chain, &url, &name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("producto %d no está en el catálogo", productID)
		}
		return err
	}

	flag := 0
	if available {
		flag = 1
	}
	stamp := time.Now()
	if _, err := tx.Exec(`
		UPDATE products
		SET price = ?, price_basis = ?, measure_price = ?, measure_unit = ?,
		    available = ?, price_fetched_at = ?
		WHERE id = ?`, price, basis, measurePrice, measureUnit, flag, ts(stamp), productID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO price_history
			(product_id, chain, product_url, name, price, measure_price, measure_unit, available, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		productID, chain, url, name, price, measurePrice, measureUnit, flag, ts(stamp)); err != nil {
		return err
	}
	return tx.Commit()
}

// priceBasis dice en qué unidad venía publicado el precio: si la tienda da un
// precio por medida, ese es el que se puede comparar con otras tiendas; si da
// un precio por unidad con el de la medida aparte, lo publicado es el de
// unidad. Sin medida publicada no se deduce ninguna: measure_price se queda a
// 0 y la base es la unidad.
// priceBasis dice en qué base está el precio que se guarda en products.price,
// que es el que la web enseña y el que se suma al total de la compra.
//
// Si la tienda publica un precio de unidad y además el €/kg, el precio es el de
// la unidad: una botella de vino a 3,65 € que además está a 4,87 €/l no es un
// producto "de base l", sigue siendo una botella. La base solo es la medida cuando
// no hay precio de unidad, que es el caso de lo vendido al peso: los plátanos a
// 1,65 €/kg no tienen precio de unidad porque no la hay.
func priceBasis(p chain.Product) string {
	if p.Price > 0 {
		return "unidad"
	}
	if p.PrecioEsPorMedida() || p.MeasurePrice > 0 {
		return p.MeasureUnit
	}
	return "unidad"
}
