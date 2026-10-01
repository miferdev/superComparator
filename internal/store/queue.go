package store

import "time"

// QueueEntry es un producto pendiente de descargar su precio. Trae la cadena y
// la URL porque quien lee la cola es quien tiene que ir a por la ficha.
type QueueEntry struct {
	ProductID     int64
	State         string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	Chain         string
	URL           string
}

// EnqueuePrices mete en la cola los productos de la cadena que aún no tienen
// precio y no estaban ya en ella. max es el tope de encolados; sin límite si
// es 0 o menos.
func (s *Store) EnqueuePrices(chain string, max int) (int, error) {
	limit := ""
	if max > 0 {
		limit = " LIMIT ?"
	}
	query := `
		INSERT INTO price_queue (product_id, state, attempts, next_attempt_at)
		SELECT p.id, 'pendiente', 0, ?
		FROM products p
		WHERE p.price = 0
		  AND (p.chain = ? OR ? = '')
		  AND p.id NOT IN (SELECT product_id FROM price_queue)
		ORDER BY p.id` + limit
	args := []any{ts(time.Now()), chain, chain}
	if max > 0 {
		args = append(args, max)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// NextQueued devuelve hasta limit entradas pendientes cuyo momento ya ha
// llegado y las marca como descargando, para que dos workers no se peleen por
// la misma ficha.
func (s *Store) NextQueued(now time.Time, limit int) ([]QueueEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT q.product_id, q.state, q.attempts, q.next_attempt_at, COALESCE(q.last_error, ''), p.chain, p.url
		FROM price_queue q JOIN products p ON p.id = q.product_id
		WHERE q.state = 'pendiente' AND q.next_attempt_at <= ?
		ORDER BY q.next_attempt_at, q.product_id
		LIMIT ?`, ts(now), limit)
	if err != nil {
		return nil, err
	}
	var out []QueueEntry
	for rows.Next() {
		var q QueueEntry
		var nextAttemptAt string
		if err := rows.Scan(&q.ProductID, &q.State, &q.Attempts, &nextAttemptAt,
			&q.LastError, &q.Chain, &q.URL); err != nil {
			rows.Close()
			return nil, err
		}
		q.NextAttemptAt = parseTime(nextAttemptAt)
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i, q := range out {
		if _, err := tx.Exec(`UPDATE price_queue SET state = 'descargando' WHERE product_id = ?`,
			q.ProductID); err != nil {
			return nil, err
		}
		out[i].State = "descargando"
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkQueueDone saca la entrada de la cola: su precio ya está descargado.
func (s *Store) MarkQueueDone(productID int64) error {
	_, err := s.db.Exec(`DELETE FROM price_queue WHERE product_id = ?`, productID)
	return err
}

// FailQueue devuelve la entrada a la cola para otro intento más, anotando los
// intentos que lleva y cuándo toca el siguiente.
func (s *Store) FailQueue(productID int64, attempts int, next time.Time, errMsg string) error {
	_, err := s.db.Exec(`
		UPDATE price_queue
		SET state = 'pendiente', attempts = ?, next_attempt_at = ?, last_error = ?
		WHERE product_id = ?`, attempts, ts(next), errMsg, productID)
	return err
}
