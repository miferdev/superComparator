package store

import "database/sql"

// querier es lo que necesita ensureColumn: ejecutar sentencias tanto sobre la
// base como dentro de una migración.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

// ensureColumn añade una columna si no existe. Permite migrar bases creadas
// con esquemas anteriores sin un sistema de versiones completo.
func ensureColumn(db querier, table, column, ddl string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + ddl)
	return err
}
