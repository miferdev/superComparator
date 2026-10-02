package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Fuentes de un precio: de dónde sale el precio de unidad con el que se suma
// una línea de la lista.
const (
	fuenteManual  = "manual"
	fuenteWeb     = "web"
	fuenteNinguno = "ninguno"
)

// ListItem es una línea de «Mi compra». Precio no es necesariamente el de la
// tienda: si el usuario puso un precio a mano, ese manda para el subtotal y
// Fuente lo dice, para que la web pueda contarlo sin adivinarlo.
//
// Subtotal es Cantidad × Precio solo si hay precio de unidad. Un producto
// vendido al peso no tiene: lo que publica es un €/kg, y multiplicarlo por la
// cantidad sería el precio de una bolsa que no se ha comprado. Esa línea lleva
// el subtotal a 0, PrecioSoloMedida a true y cuenta en SinPrecio.
type ListItem struct {
	ID           int64
	ProductID    int64
	Chain        string
	NombreCadena string
	ProductURL   string
	Nombre       string
	Formato      string
	Cantidad     float64
	// Precio efectivo: el manual si lo hay, si no el de la tienda.
	Precio           float64
	Fuente           string // "manual", "web" o "ninguno"
	PrecioMedida     float64
	Medida           string
	PrecioSoloMedida bool
	Subtotal         float64 // Cantidad * Precio, 0 si no hay precio de unidad
	Añadido          time.Time
}

// ListaCompra es la lista con sus subtotales. SubtotalPorCadena va por
// identificador de cadena y SubtotalPorCadenaNombres por el nombre que se
// enseña, para que quien pinte la lista no tenga que cruzar con la tabla de
// cadenas. Total solo suma precios de unidad: SinPrecio dice cuántas líneas se
// quedan fuera y por qué no se pueden sumar.
type ListaCompra struct {
	Items                    []ListItem
	SubtotalPorCadena        map[string]float64
	SubtotalPorCadenaNombres map[string]string
	Total                    float64
	SinPrecio                int // cuántas líneas no se pueden sumar (solo precio por medida)
}

// Origen de una línea de la lista. Es una consulta, no una por línea: con
// muchas líneas eso se nota.
//
// El precio efectivo se resuelve aquí y no en Go para que la fila traiga ya el
// precio manual si lo hay. Lo que se busca es m.chain (no el precio) porque un
// precio manual a 0 es un precio manual, no un precio que falta.
const (
	listColumns = `i.id, p.id, p.chain, COALESCE(c.nombre, ''), p.url, p.name, COALESCE(p.format, ''),
		i.quantity,
		CASE WHEN m.chain IS NOT NULL THEN m.precio ELSE p.price END,
		CASE WHEN m.chain IS NOT NULL THEN 1 ELSE 0 END,
		CASE WHEN m.chain IS NOT NULL THEN m.precio_medida ELSE p.measure_price END,
		CASE WHEN m.chain IS NOT NULL THEN m.medida ELSE p.measure_unit END,
		i.added_at` + "\n"
	listFrom = `FROM list_items i
		JOIN products p ON p.id = i.product_id
		LEFT JOIN chains c ON c.id = p.chain
		LEFT JOIN precios_manuales m ON m.chain = p.chain AND m.product_url = p.url`
	listSelect = `SELECT ` + listColumns + listFrom
)

func scanListItem(sc interface{ Scan(dest ...any) error }) (ListItem, error) {
	var it ListItem
	var manual int
	var anadido string
	if err := sc.Scan(&it.ID, &it.ProductID, &it.Chain, &it.NombreCadena, &it.ProductURL,
		&it.Nombre, &it.Formato, &it.Cantidad, &it.Precio, &manual, &it.PrecioMedida,
		&it.Medida, &anadido); err != nil {
		return ListItem{}, err
	}
	it.Fuente = fuenteDePrecio(manual == 1, it.Precio)
	it.PrecioSoloMedida = it.Precio <= 0 && it.PrecioMedida > 0
	if it.Precio > 0 {
		it.Subtotal = it.Cantidad * it.Precio
	}
	it.Añadido = parseTime(anadido)
	return it, nil
}

// fuenteDePrecio dice de dónde sale el precio de unidad de una línea. "ninguno"
// es para lo que no tiene precio de unidad: lo vendido al peso, que solo tiene
// €/kg, y lo que aún no se ha descargado de la tienda.
func fuenteDePrecio(manual bool, precio float64) string {
	switch {
	case manual:
		return fuenteManual
	case precio > 0:
		return fuenteWeb
	default:
		return fuenteNinguno
	}
}

// AddToList mete un producto en la lista con la cantidad indicada y devuelve la
// línea ya resuelta (con el precio manual si lo hay). La posición es la última
// más uno, que es lo que mantiene el orden en que se fue añadiendo.
//
// Si el producto ya estaba en la lista devuelve error: la línea se cambia con
// SetListQuantity, así que aquí no se decide por sorpresa cuánto sumar.
func (s *Store) AddToList(chainID, productURL string, cantidad float64) (ListItem, error) {
	if cantidad <= 0 {
		return ListItem{}, fmt.Errorf("la cantidad tiene que ser mayor que cero (es %.2f)", cantidad)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return ListItem{}, err
	}
	defer tx.Rollback()

	var productID int64
	if err := tx.QueryRow(`SELECT id FROM products WHERE chain = ? AND url = ?`,
		chainID, productURL).Scan(&productID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ListItem{}, fmt.Errorf("el producto %s de %s no está en el catálogo", productURL, chainID)
		}
		return ListItem{}, err
	}

	var enLaLista int
	if err := tx.QueryRow(`SELECT count(*) FROM list_items WHERE product_id = ?`, productID).
		Scan(&enLaLista); err != nil {
		return ListItem{}, err
	}
	if enLaLista > 0 {
		return ListItem{}, fmt.Errorf("%s ya está en la lista; cambia su cantidad en vez de añadirlo otra vez", productURL)
	}

	if _, err := tx.Exec(`
		INSERT INTO list_items (product_id, quantity, position, added_at)
		VALUES (?, ?, (SELECT COALESCE(max(position), 0) + 1 FROM list_items), ?)`,
		productID, cantidad, ts(time.Now())); err != nil {
		return ListItem{}, err
	}

	row := tx.QueryRow(listSelect+` WHERE i.product_id = ?`, productID)
	it, err := scanListItem(row)
	if err != nil {
		return ListItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return ListItem{}, err
	}
	return it, nil
}

// SetListQuantity cambia la cantidad de una línea. Una cantidad de cero o menos
// no vale: para quitar un producto de la lista está RemoveFromList, y dejar una
// línea a cero haría que el total pareciera estar completo sin serlo.
func (s *Store) SetListQuantity(chainID, productURL string, cantidad float64) error {
	if cantidad <= 0 {
		return fmt.Errorf("la cantidad tiene que ser mayor que cero (es %.2f)", cantidad)
	}
	res, err := s.db.Exec(`
		UPDATE list_items SET quantity = ?
		WHERE product_id = (SELECT id FROM products WHERE chain = ? AND url = ?)`,
		cantidad, chainID, productURL)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s no está en la lista", productURL)
	}
	return nil
}

// RemoveFromList quita una línea de la lista. Si no estaba, no pasa nada.
func (s *Store) RemoveFromList(chainID, productURL string) error {
	_, err := s.db.Exec(`
		DELETE FROM list_items
		WHERE product_id = (SELECT id FROM products WHERE chain = ? AND url = ?)`,
		chainID, productURL)
	return err
}

// ClearList vacía la lista entera.
func (s *Store) ClearList() error {
	_, err := s.db.Exec(`DELETE FROM list_items`)
	return err
}

// ListaCompra devuelve la lista con sus subtotales. El total solo suma líneas
// con precio de unidad: las que solo tienen precio por medida no se pueden
// sumar y se cuentan aparte, para que el total sea el que se ha gastado de
// verdad y no una estimación.
func (s *Store) ListaCompra() (ListaCompra, error) {
	rows, err := s.db.Query(listSelect + ` ORDER BY i.position, i.id`)
	if err != nil {
		return ListaCompra{}, err
	}
	defer rows.Close()

	items := []ListItem{}
	subtotales := map[string]float64{}
	nombres := map[string]string{}
	var total float64
	sinPrecio := 0
	for rows.Next() {
		it, err := scanListItem(rows)
		if err != nil {
			return ListaCompra{}, err
		}
		items = append(items, it)
		nombres[it.Chain] = it.NombreCadena
		if it.Precio > 0 {
			subtotales[it.Chain] += it.Subtotal
			total += it.Subtotal
		} else {
			sinPrecio++
		}
	}
	if err := rows.Err(); err != nil {
		return ListaCompra{}, err
	}
	return ListaCompra{
		Items:                    items,
		SubtotalPorCadena:        subtotales,
		SubtotalPorCadenaNombres: nombres,
		Total:                    total,
		SinPrecio:                sinPrecio,
	}, nil
}
