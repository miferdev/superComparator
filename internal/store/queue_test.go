package store

import (
	"path/filepath"
	"testing"
	"time"
)

func productoID(t *testing.T, st *Store, chainID, url string) int64 {
	t.Helper()
	p, ok, err := st.Product(chainID, url)
	if err != nil || !ok {
		t.Fatalf("Product(%s, %s): %v %v", chainID, url, ok, err)
	}
	return p.ID
}

// sembrarPrecios cataloga una ficha por URL en cada cadena y las encola.
func sembrarPrecios(t *testing.T, st *Store, porCadena map[string][]string) {
	t.Helper()
	for chainID, urls := range porCadena {
		for _, url := range urls {
			if _, _, err := st.UpsertProducts(chainID, []Product{{URL: url, Name: "Producto " + url}}); err != nil {
				t.Fatalf("UpsertProducts(%s): %v", chainID, err)
			}
		}
		if _, err := st.EnqueuePrices(chainID, 0); err != nil {
			t.Fatalf("EnqueuePrices(%s): %v", chainID, err)
		}
	}
}

func estadoDe(t *testing.T, st *Store, productID int64) (string, int, string, string) {
	t.Helper()
	var (
		estado, next, ultimo string
		attempts             int
	)
	err := st.db.QueryRow(`SELECT state, attempts, next_attempt_at, COALESCE(last_error, '')
		FROM price_queue WHERE product_id = ?`, productID).Scan(&estado, &attempts, &next, &ultimo)
	if err != nil {
		t.Fatalf("cola del producto %d: %v", productID, err)
	}
	return estado, attempts, next, ultimo
}

// ---- cola: rescate de fichas atascadas ----------------------------------

// Un proceso que muere con fichas en 'descargando' las deja bloqueadas para
// siempre: hay que rescatarlas o la cola no avanza.
func TestReclaimStaleRescataSoloLoViejo(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"vieja", "nueva", "sin_marca"}})

	colocar := func(url, updatedAt string) {
		t.Helper()
		_, err := st.db.Exec(`UPDATE price_queue SET state = 'descargando', updated_at = ?
			WHERE product_id = ?`, updatedAt, productoID(t, st, "mercadona", url))
		if err != nil {
			t.Fatalf("colocar %s: %v", url, err)
		}
	}
	colocar("vieja", ts(time.Now().Add(-10*time.Minute)))
	colocar("nueva", ts(time.Now().Add(-10*time.Second)))
	// Sin marca (lo que dejó una base de antes de la migración 2) es anterior a
	// cualquier marca real, así que también se da por atascada.
	colocar("sin_marca", "")

	n, err := st.ReclaimStale(time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if n != 2 {
		t.Fatalf("rescatadas = %d, quiero 2 (la vieja y la sin marca)", n)
	}

	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 2 || stats[estadoDescargando] != 1 {
		t.Fatalf("stats = %+v", stats)
	}

	// La rescatada vuelve a salir; la que se está descargando, no.
	cola, err := st.NextQueuedChain(time.Now().Add(time.Hour), "mercadona", 10)
	if err != nil {
		t.Fatalf("NextQueuedChain: %v", err)
	}
	if len(cola) != 2 {
		t.Fatalf("cola = %+v", cola)
	}

	// Con un ttl enorme no se toca nada.
	if n, err := st.ReclaimStale(time.Hour); err != nil || n != 0 {
		t.Fatalf("ReclaimStale enorme = %d, %v", n, err)
	}
}

// ---- cola: backoff y límite de intentos ---------------------------------

// Agotados los intentos, la cola deja de ofrecerla sola: hasta que alguien la
// rearme con Requeue.
func TestMaxIntentosDejaLaFichaEnErrorYRequeueLaRearma(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	id := productoID(t, st, "mercadona", "m1")

	for intento := 1; intento <= MaxIntentos; intento++ {
		estado, err := st.FailQueue(id, intento, time.Now(), "la web no responde")
		quiero := estadoPendiente
		if intento == MaxIntentos {
			quiero = estadoError
		}
		if err != nil || estado != quiero {
			t.Fatalf("intento %d = %q (%v), quiero %q", intento, estado, err, quiero)
		}
	}

	// Agotados los intentos, la cola deja de ofrecerla sola.
	if cola, err := st.NextQueuedChain(time.Now().Add(time.Hour), "mercadona", 10); err != nil || len(cola) != 0 {
		t.Fatalf("una ficha en error sigue saliendo sola: %+v, %v", cola, err)
	}
	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoError] != 1 || stats[estadoPendiente] != 0 {
		t.Fatalf("stats = %+v", stats)
	}

	// Rearmarla la deja pendiente, con los intentos a cero y sin error.
	if err := st.Requeue(id); err != nil {
		t.Fatalf("Requeue: %v", err)
	}
	if stats, err = st.QueueStats("mercadona"); err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 1 || stats[estadoError] != 0 {
		t.Fatalf("stats tras rearmar = %+v", stats)
	}
	cola, err := st.NextQueuedChain(time.Now().Add(time.Hour), "mercadona", 10)
	if err != nil || len(cola) != 1 {
		t.Fatalf("cola tras rearmar = %+v, %v", cola, err)
	}
	if cola[0].Attempts != 0 || cola[0].LastError != "" {
		t.Fatalf("la rearmada conserva el historial del fallo: %+v", cola[0])
	}
}

func TestQueueStatsCuentaPorCadenaYPorEstado(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1", "m2"}, "dia": {"d1"}})
	if estado, err := st.FailQueue(productoID(t, st, "mercadona", "m1"), MaxIntentos,
		time.Now(), "caida"); err != nil || estado != estadoError {
		t.Fatalf("FailQueue = %q, %v", estado, err)
	}

	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 1 || stats[estadoError] != 1 || stats[estadoDescargando] != 0 {
		t.Fatalf("mercadona = %+v", stats)
	}
	stats, err = st.QueueStats("dia")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 1 {
		t.Fatalf("dia = %+v", stats)
	}
	// Una cadena sin nada da ceros en vez de fallar.
	if stats, err = st.QueueStats("ahorramas"); err != nil || stats[estadoPendiente] != 0 {
		t.Fatalf("ahorramas = %+v, %v", stats, err)
	}
}

// Una base creada antes de la migración 2 tiene que seguir abriéndose: la
// migración añade la marca, rellena lo que ya estaba en la cola y deja
// rescatable lo que un proceso muerto dejó a medias.
func TestMigracion2SobreUnaBaseVieja(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalogo.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1", "m2"}})

	// Dejar la base como estaba antes de la migración 2.
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_queue_estado`,
		`ALTER TABLE price_queue RENAME TO price_queue_vieja`,
		`CREATE TABLE price_queue (
  product_id        INTEGER PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  state             TEXT NOT NULL DEFAULT 'pendiente',
  attempts          INTEGER NOT NULL DEFAULT 0,
  next_attempt_at   TEXT NOT NULL,
  last_error        TEXT
)`,
		`INSERT INTO price_queue (product_id, state, attempts, next_attempt_at, last_error)
		SELECT product_id, state, attempts, next_attempt_at, last_error FROM price_queue_vieja`,
		`DROP TABLE price_queue_vieja`,
		`DELETE FROM schema_migrations WHERE version = 2`,
	} {
		if _, err := st.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reabrir con la migración 2: %v", err)
	}
	defer st.Close()
	var marcadas int
	if err := st.db.QueryRow(`SELECT count(*) FROM price_queue WHERE updated_at <> ''`).
		Scan(&marcadas); err != nil {
		t.Fatalf("marcas: %v", err)
	}
	if marcadas != 2 {
		t.Fatalf("fichas con marca = %d, quiero 2", marcadas)
	}
	// Lo que se había quedado en 'descargando' es justo lo que hay que rescatar.
	if _, err := st.db.Exec(`UPDATE price_queue SET state = 'descargando', updated_at = ?
		WHERE product_id = ?`, ts(time.Now().Add(-time.Hour)), productoID(t, st, "mercadona", "m1")); err != nil {
		t.Fatalf("atascar: %v", err)
	}
	if _, err := st.ReclaimStale(time.Minute); err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if estado, _, _, _ := estadoDe(t, st, productoID(t, st, "mercadona", "m1")); estado != estadoPendiente {
		t.Fatalf("estado = %q, quiero %q", estado, estadoPendiente)
	}
}
