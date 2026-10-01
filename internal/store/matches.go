package store

type Match struct {
	ItemID      int64
	ItemName    string
	Quantity    int
	Chain       string
	URL         string
	SKU         string
	MatchedName string
	Score       float64
	Available   bool
}

// ClearMatch borra el match y las alternativas de un producto en una cadena.
// Se llama antes de volver a resolver: si el producto ya no encaja o la
// resolución falla, lo que había se queda sin precio en lugar de sobrevivir de
// una versión anterior de las reglas.
func (s *Store) ClearMatch(itemID int64, chainID string) error {
	if _, err := s.db.Exec(`DELETE FROM matches WHERE item_id = ? AND chain = ?`, itemID, chainID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM alternatives WHERE item_id = ? AND chain = ?`, itemID, chainID)
	return err
}

// ClearMatches borra todos los matches y alternativas. Se usa cuando cambian las
// reglas de coincidencia: lo guardado era decisión de otra versión y no puede
// seguir mezclándose con los productos nuevos.
func (s *Store) ClearMatches() error {
	if _, err := s.db.Exec(`DELETE FROM matches`); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM alternatives`)
	return err
}
