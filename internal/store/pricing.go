package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
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
		SET price = ?, price_basis = ?, measure_price = ?, available = ?, price_fetched_at = ?
		WHERE id = ?`, price, basis, measurePrice, flag, ts(stamp), productID); err != nil {
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
