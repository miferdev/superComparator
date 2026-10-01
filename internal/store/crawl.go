package store

import (
	"database/sql"
	"errors"
	"time"
)

// CrawlState es el punto donde se quedó el rastreo de una cadena, para poder
// seguirlo donde lo dejó en vez de empezar de cero.
type CrawlState struct {
	Chain     string
	Phase     string
	Cursor    string
	Done      int
	Total     int
	Paused    bool
	UpdatedAt time.Time
}

// CrawlState devuelve el estado del rastreo de una cadena. Una cadena que aún
// no se ha rastreado devuelve el estado vacío.
func (s *Store) CrawlState(chain string) (CrawlState, error) {
	var st CrawlState
	var paused int
	var updatedAt string
	err := s.db.QueryRow(`
		SELECT chain, phase, cursor, done, total, paused, updated_at
		FROM crawl_state WHERE chain = ?`, chain).
		Scan(&st.Chain, &st.Phase, &st.Cursor, &st.Done, &st.Total, &paused, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CrawlState{Chain: chain}, nil
		}
		return CrawlState{}, err
	}
	st.Paused = paused == 1
	st.UpdatedAt = parseTime(updatedAt)
	return st, nil
}

// SetCrawlState guarda el punto en el que está el rastreo de una cadena.
func (s *Store) SetCrawlState(st CrawlState) error {
	paused := 0
	if st.Paused {
		paused = 1
	}
	updated := st.UpdatedAt
	if updated.IsZero() {
		updated = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO crawl_state (chain, phase, cursor, done, total, paused, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chain) DO UPDATE SET
			phase = excluded.phase,
			cursor = excluded.cursor,
			done = excluded.done,
			total = excluded.total,
			paused = excluded.paused,
			updated_at = excluded.updated_at`,
		st.Chain, st.Phase, st.Cursor, st.Done, st.Total, paused, ts(updated))
	return err
}
