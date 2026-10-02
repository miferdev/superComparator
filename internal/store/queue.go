package store

import "time"

// MaxIntentos es a partir de cuándo una ficha se marca como 'error' y deja de
// reintentarse sola, hasta que alguien la rearme.
const MaxIntentos = 5

// Estados de la cola. 'hecho' no está entre ellos porque una ficha terminada
// sale de la cola: MarkQueueDone la borra.
const (
	estadoPendiente   = "pendiente"
	estadoDescargando = "descargando"
	estadoError       = "error"
)

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
		INSERT INTO price_queue (product_id, state, attempts, next_attempt_at, updated_at)
		SELECT p.id, 'pendiente', 0, ?, ?
		FROM products p
		WHERE p.price = 0
		  AND (p.chain = ? OR ? = '')
		  AND p.id NOT IN (SELECT product_id FROM price_queue)
		ORDER BY p.id` + limit
	stamp := ts(time.Now())
	args := []any{stamp, stamp, chain, chain}
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
	return s.nextQueued(now, "", limit)
}

// NextQueuedChain es NextQueued para una sola cadena: el worker va por turnos,
// una cadena cada vez, para que el catálogo no se llene de una sola tienda
// mientras las demás esperan.
func (s *Store) NextQueuedChain(now time.Time, chainID string, limit int) ([]QueueEntry, error) {
	return s.nextQueued(now, chainID, limit)
}

// nextQueued reserva entradas pendientes. chainID vacía significa todas.
func (s *Store) nextQueued(momento time.Time, chainID string, limit int) ([]QueueEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	filtro := ""
	args := []any{ts(momento)}
	if chainID != "" {
		filtro = " AND p.chain = ?"
		args = append(args, chainID)
	}
	args = append(args, limit)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT q.product_id, q.state, q.attempts, q.next_attempt_at, COALESCE(q.last_error, ''), p.chain, p.url
		FROM price_queue q JOIN products p ON p.id = q.product_id
		WHERE q.state = 'pendiente' AND q.next_attempt_at <= ?`+filtro+`
		ORDER BY q.next_attempt_at, q.product_id
		LIMIT ?`, args...)
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
		// La marca del cambio es lo que luego permite distinguir una descarga en
		// curso de una que se quedó a medias al morir el proceso.
		if _, err := tx.Exec(`UPDATE price_queue SET state = 'descargando', updated_at = ?
			WHERE product_id = ?`, ts(momento), q.ProductID); err != nil {
			return nil, err
		}
		out[i].State = estadoDescargando
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReclaimStale devuelve a 'pendiente' las fichas que llevan más de ttl
// atascadas en 'descargando' (el proceso murió con ellas a medias) y
// devuelve cuántas ha rescatado.
func (s *Store) ReclaimStale(ttl time.Duration) (int, error) {
	now := time.Now()
	// Las marcas son texto RFC3339 en UTC, así que compararlas ordena por tiempo.
	// Una marca vacía (lo que quedó de una base anterior a la migración 2) es
	// anterior a cualquier marca real, de modo que también se da por atascada y
	// se rescata. Los intentos no se tocan: el rescate es por trabajo perdido,
	// no por un fallo de la tienda, y sumarlos dejaría fichas buenas en 'error'.
	res, err := s.db.Exec(`
		UPDATE price_queue SET state = 'pendiente', updated_at = ?
		WHERE state = 'descargando' AND updated_at < ?`,
		ts(now), ts(now.Add(-ttl)))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// MarkQueueDone saca la entrada de la cola: su precio ya está descargado.
func (s *Store) MarkQueueDone(productID int64) error {
	_, err := s.db.Exec(`DELETE FROM price_queue WHERE product_id = ?`, productID)
	return err
}

// FailQueue sube el intento, guarda el error y, si se supera MaxIntentos, deja
// la ficha en 'error'. Devuelve el estado en el que queda.
func (s *Store) FailQueue(productID int64, attempts int, next time.Time, errMsg string) (string, error) {
	state := estadoPendiente
	if attempts >= MaxIntentos {
		state = estadoError
	}
	_, err := s.db.Exec(`
		UPDATE price_queue
		SET state = ?, attempts = ?, next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE product_id = ?`,
		state, attempts, ts(next), errMsg, ts(time.Now()), productID)
	if err != nil {
		return "", err
	}
	return state, nil
}

// Requeue deja una ficha en 'error' otra vez pendiente, con los intentos a
// cero: es lo que hará la API cuando el usuario decida rearmar un producto.
func (s *Store) Requeue(productID int64) error {
	stamp := ts(time.Now())
	_, err := s.db.Exec(`
		UPDATE price_queue
		SET state = 'pendiente', attempts = 0, next_attempt_at = ?, last_error = NULL, updated_at = ?
		WHERE product_id = ?`, stamp, stamp, productID)
	return err
}

// QueueStats cuenta las fichas de una cadena por estado.
func (s *Store) QueueStats(chainID string) (map[string]int, error) {
	out := map[string]int{
		estadoPendiente:   0,
		estadoDescargando: 0,
		estadoError:       0,
	}
	rows, err := s.db.Query(`
		SELECT q.state, count(*)
		FROM price_queue q JOIN products p ON p.id = q.product_id
		WHERE p.chain = ?
		GROUP BY q.state`, chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			state string
			n     int
		)
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[state] = n
	}
	return out, rows.Err()
}
