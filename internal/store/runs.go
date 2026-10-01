package store

import (
	"database/sql"
	"errors"
	"time"
)

// Run es una ejecución del programa o de una parte suya, para poder saber
// cuándo se.catalogó por última vez y cómo acabó.
type Run struct {
	ID         int64
	Kind       string
	Chain      string
	StartedAt  time.Time
	FinishedAt time.Time
	Products   int
	Errors     int
	Note       string
}

// StartRun registra el comienzo de una ejecución y devuelve su identificador.
func (s *Store) StartRun(kind, chain string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO runs (kind, chain, started_at) VALUES (?, ?, ?)`,
		kind, chain, ts(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRun cierra la ejecución con lo que hizo y con cómo acabó.
func (s *Store) FinishRun(id int64, products, errs int, note string) error {
	_, err := s.db.Exec(`
		UPDATE runs SET finished_at = ?, products = ?, errors = ?, note = ? WHERE id = ?`,
		ts(time.Now()), products, errs, note, id)
	return err
}

// LastRun devuelve la última ejecución registrada, exista o no.
func (s *Store) LastRun() (Run, bool, error) {
	row := s.db.QueryRow(`
		SELECT id, kind, chain, started_at, COALESCE(finished_at, ''), products, errors, COALESCE(note, '')
		FROM runs ORDER BY started_at DESC, id DESC LIMIT 1`)
	var r Run
	var startedAt, finishedAt string
	if err := row.Scan(&r.ID, &r.Kind, &r.Chain, &startedAt, &finishedAt,
		&r.Products, &r.Errors, &r.Note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, false, nil
		}
		return Run{}, false, err
	}
	r.StartedAt = parseTime(startedAt)
	r.FinishedAt = parseTime(finishedAt)
	return r, true, nil
}
