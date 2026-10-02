package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PrecioManual es un precio que ha puesto el usuario, no la tienda. Existe para
// las cadenas cuyo precio no se puede descargar (Alcampo responde 403) y para
// los precios que se han visto en el lineal. Es un dato de otra fuente, así que
// vive aparte de products.price y de price_history, que son lo que publica la
// tienda: ninguno pisa al otro.
//
// Precio es el precio de unidad y PrecioMedida el €/kg o €/l, con Medida a
// "kg", "l" o "" si no hay. Igual que en el catálogo, un producto que solo se
// publica por medida tiene Precio a 0 a propósito.
type PrecioManual struct {
	Chain        string
	ProductURL   string
	Precio       float64
	PrecioMedida float64
	Medida       string
	Nota         string
	Actualizado  time.Time
}

const precioManualColumns = `chain, product_url, precio, precio_medida, medida, nota, actualizado`

func scanPrecioManual(sc interface{ Scan(dest ...any) error }) (PrecioManual, error) {
	var m PrecioManual
	var actualizado string
	if err := sc.Scan(&m.Chain, &m.ProductURL, &m.Precio, &m.PrecioMedida, &m.Medida,
		&m.Nota, &actualizado); err != nil {
		return PrecioManual{}, err
	}
	m.Actualizado = parseTime(actualizado)
	return m, nil
}

// medidasValidas son las únicas medidas que se guardan: las que se pueden sumar
// al total sin inventar nada. Cualquier otra cosa no se acepta, porque un
// "€ por bolsa" no se puede comparar con un €/kg.
var medidasValidas = map[string]bool{"kg": true, "l": true}

// SetPrecioManual guarda el precio que ha puesto el usuario para un producto. Es
// un upsert: volver a poner precio para la misma ficha lo deja en una fila, con
// la marca de ahora.
//
// Antes de escribir valida, y si algo no cuadra devuelve error sin tocar la base:
// un precio guardado a medias es peor que no guardar nada, porque el usuario
// cree que lo tiene guardado. Valida que
//
//   - el producto exista en el catálogo,
//   - haya un precio de unidad, o un precio por medida con su medida,
//   - si hay precio por medida, la medida sea kg o l,
//   - y que no se pueda guardar una medida sin precio de medida.
//
// Actualizado de la entrada se ignora: lo pone SetPrecioManual.
func (s *Store) SetPrecioManual(m PrecioManual) error {
	medida := strings.ToLower(strings.TrimSpace(m.Medida))
	if err := validarPrecioManual(m, medida); err != nil {
		return err
	}

	var existe int
	if err := s.db.QueryRow(`SELECT count(*) FROM products WHERE chain = ? AND url = ?`,
		m.Chain, m.ProductURL).Scan(&existe); err != nil {
		return err
	}
	if existe == 0 {
		return fmt.Errorf("el producto %s de %s no está en el catálogo", m.ProductURL, m.Chain)
	}

	_, err := s.db.Exec(`
		INSERT INTO precios_manuales
			(chain, product_url, precio, precio_medida, medida, nota, actualizado)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chain, product_url) DO UPDATE SET
			precio = excluded.precio,
			precio_medida = excluded.precio_medida,
			medida = excluded.medida,
			nota = excluded.nota,
			actualizado = excluded.actualizado`,
		m.Chain, m.ProductURL, m.Precio, m.PrecioMedida, medida, m.Nota, ts(time.Now()))
	return err
}

func validarPrecioManual(m PrecioManual, medida string) error {
	if medida != "" && m.PrecioMedida <= 0 {
		return fmt.Errorf("no se puede guardar la medida %q sin un precio por medida", medida)
	}
	if m.PrecioMedida > 0 && !medidasValidas[medida] {
		if medida == "" {
			return errors.New("un precio por medida necesita su medida: kg o l")
		}
		return fmt.Errorf("la medida tiene que ser kg o l, no %q", medida)
	}
	if m.Precio <= 0 && !(m.PrecioMedida > 0 && medida != "") {
		return errors.New("hace falta un precio de unidad o un precio por medida")
	}
	return nil
}

// GetPrecioManual devuelve el precio que puso el usuario para una ficha. El
// segundo valor dice si lo hay: que la tienda tenga precio no cuenta, porque
// este es solo el dato del usuario.
func (s *Store) GetPrecioManual(chainID, productURL string) (PrecioManual, bool, error) {
	row := s.db.QueryRow(`SELECT `+precioManualColumns+
		` FROM precios_manuales WHERE chain = ? AND product_url = ?`, chainID, productURL)
	m, err := scanPrecioManual(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PrecioManual{}, false, nil
		}
		return PrecioManual{}, false, err
	}
	return m, true, nil
}

// PreciosManuales devuelve todos los precios puestos a mano.
func (s *Store) PreciosManuales() ([]PrecioManual, error) {
	rows, err := s.db.Query(`SELECT ` + precioManualColumns +
		` FROM precios_manuales ORDER BY chain, product_url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrecioManual
	for rows.Next() {
		m, err := scanPrecioManual(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// BorrarPrecioManual quita el precio puesto a mano y deja el producto como
// estaba: el precio de la tienda no se toca en ningún momento. Si no había
// ninguno no pasa nada, para que quitarlo sea idempotente.
func (s *Store) BorrarPrecioManual(chainID, productURL string) error {
	_, err := s.db.Exec(`DELETE FROM precios_manuales WHERE chain = ? AND product_url = ?`,
		chainID, productURL)
	return err
}
