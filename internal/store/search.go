package store

import "strings"

// SearchQuery son los criterios de una búsqueda del catálogo. Text vacío
// devuelve el catálogo entero, que es lo que quiere el listado.
type SearchQuery struct {
	Text      string
	Chain     string
	ConPrecio bool
	Orden     string // "relevancia" (defecto), "precio_asc", "precio_desc", "nombre", "medida_asc"
	Offset    int
	Limit     int
}

// SearchResult es una página de resultados y si queda más detrás.
type SearchResult struct {
	Products []Product
	Total    int
	HayMas   bool
}

const (
	searchLimitDefault = 50
	searchLimitMax     = 200
	searchOffsetMax    = 2000
	searchTermsMax     = 8
	searchTermLen      = 32
)

// acentos quita lo que impide comparar nombres: el texto de las fichas trae
// tildes y ñ, y quien busca no suele escribirlos.
var acentos = strings.NewReplacer(
	"á", "a", "à", "a", "ä", "a", "â", "a", "å", "a", "ā", "a",
	"é", "e", "è", "e", "ë", "e", "ê", "e", "ē", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i", "ī", "i",
	"ó", "o", "ò", "o", "ö", "o", "ô", "o", "ō", "o",
	"ú", "u", "ù", "u", "ü", "u", "û", "u", "ū", "u",
	"ñ", "n", "ç", "c", "ý", "y",
)

// terminos parte lo que se ha escrito en palabras utilizables por FTS5.
func terminos(text string) []string {
	var out []string
	for _, word := range strings.FieldsFunc(acentos.Replace(strings.ToLower(text)), separadorRune) {
		if r := []rune(word); len(r) > searchTermLen {
			word = string(r[:searchTermLen])
		}
		if word == "" {
			continue
		}
		out = append(out, word)
		if len(out) == searchTermsMax {
			break
		}
	}
	return out
}

func isTerminoRune(r rune) bool {
	return r >= 0x80 || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// separadorRune marca todo lo que no sirve para buscar: comas, puntos, paréntesis.
func separadorRune(r rune) bool { return !isTerminoRune(r) }

// ftsQuery monta la consulta de FTS5. Cada palabra va entre comillas y con
// comodín de prefijo: así "fres" encuentra "fresas" y una palabra rara no
// rompe la sintaxis.
func ftsQuery(terms []string) string {
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, `"`+t+`"*`)
	}
	return strings.Join(quoted, " AND ")
}

// Search busca productos en el catálogo por texto y filtros. Si FTS5 rechaza la
// consulta cae a un LIKE sobre el nombre de búsqueda en vez de devolver error.
func (s *Store) Search(q SearchQuery) (SearchResult, error) {
	terms := terminos(q.Text)
	res, err := s.search(q, terms, true)
	if err == nil || len(terms) == 0 {
		return res, err
	}
	return s.search(q, terms, false)
}

func (s *Store) search(q SearchQuery, terms []string, useFTS bool) (SearchResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = searchLimitDefault
	}
	if limit > searchLimitMax {
		limit = searchLimitMax
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > searchOffsetMax {
		offset = searchOffsetMax
	}

	from := "FROM products p"
	var (
		where []string
		args  []any
	)
	if useFTS && len(terms) > 0 {
		from = "FROM products_fts JOIN products p ON p.id = products_fts.rowid"
		where = append(where, "products_fts MATCH ?")
		args = append(args, ftsQuery(terms))
	} else if !useFTS && len(terms) > 0 {
		where = append(where, "p.search_name LIKE ?")
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(q.Text))+"%")
	}
	if q.Chain != "" {
		where = append(where, "p.chain = ?")
		args = append(args, q.Chain)
	}
	if q.ConPrecio {
		where = append(where, "p.price > 0")
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRow(`SELECT count(*) `+from+cond, args...).Scan(&total); err != nil {
		return SearchResult{}, err
	}
	rows, err := s.db.Query(`SELECT `+productColumns+` `+from+cond+
		` ORDER BY `+ordenClause(q.Orden, useFTS && len(terms) > 0)+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return SearchResult{}, err
	}
	defer rows.Close()
	out := make([]Product, 0, limit)
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return SearchResult{}, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Products: out, Total: total, HayMas: total > offset+len(out)}, nil
}

func ordenClause(orden string, conFTS bool) string {
	switch orden {
	case "precio_asc":
		return "p.price ASC, p.name COLLATE NOCASE"
	case "precio_desc":
		return "p.price DESC, p.name COLLATE NOCASE"
	case "nombre":
		return "p.name COLLATE NOCASE"
	case "medida_asc":
		return "p.measure_price ASC, p.name COLLATE NOCASE"
	}
	if conFTS {
		return "bm25(products_fts)"
	}
	return "p.name COLLATE NOCASE"
}
