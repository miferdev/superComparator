package server

import (
	"fmt"
	"net/http"

	"github.com/miferdev/superComparator/internal/catalog"
)

// peticionCompra es lo que manda la web al añadir un producto o al cambiar su
// cantidad. Cantidad va como puntero para poder distinguir "no lo mandé" de
// "mandé cero": si no se manda, al añadir se cuenta 1 (que es lo que quiere
// decir el botón de "añadir") y al cambiar es un 400, porque un PUT sin
// cantidad no puede querer decir "quítalo" y quitándolo en silencio.
type peticionCompra struct {
	Cadena   string   `json:"cadena"`
	URL      string   `json:"url"`
	Cantidad *float64 `json:"cantidad"`
}

// respuestaMiCompra es la MiCompraDTO tal cual, con un texto más: qué son las
// líneas que no se suman al total. Sin eso el total parece completo cuando no
// lo está, y el usuario no tiene por dónde enterarse.
type respuestaMiCompra struct {
	catalog.MiCompraDTO
	AvisoTotal string `json:"avisoTotal"`
}

// GET /api/mi-compra
func (s *Server) miCompra(w http.ResponseWriter, _ *http.Request) {
	compra, err := s.catalog.MiCompra()
	if err != nil {
		writeErr(w, err)
		return
	}
	s.escribirMiCompra(w, http.StatusOK, compra)
}

// POST /api/mi-compra
//
// Responde 201 con la lista entera ya actualizada: la web acaba de cambiarla y
// tener que volver a pedirla sería tirar el trabajo que acaba de hacer.
func (s *Server) anadirALaCompra(w http.ResponseWriter, r *http.Request) {
	var p peticionCompra
	if !decodeJSON(w, r, &p) {
		return
	}
	if !exigirProducto(w, p.Cadena, p.URL) {
		return
	}
	cantidad := 1.0
	if p.Cantidad != nil {
		cantidad = *p.Cantidad
	}
	if err := s.catalog.AñadirALaCompra(p.Cadena, p.URL, cantidad); err != nil {
		writeErrDominio(w, err)
		return
	}
	compra, err := s.catalog.MiCompra()
	if err != nil {
		writeErr(w, err)
		return
	}
	s.escribirMiCompra(w, http.StatusCreated, compra)
}

// PUT /api/mi-compra
//
// Cantidad 0 o menor quita el producto, que es lo que quiere decir el usuario;
// la quitar no se decide aquí, la hace catalog.CambiarCantidad.
func (s *Server) cambiarCantidad(w http.ResponseWriter, r *http.Request) {
	var p peticionCompra
	if !decodeJSON(w, r, &p) {
		return
	}
	if !exigirProducto(w, p.Cadena, p.URL) {
		return
	}
	if p.Cantidad == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hace falta la cantidad"})
		return
	}
	if err := s.catalog.CambiarCantidad(p.Cadena, p.URL, *p.Cantidad); err != nil {
		writeErrDominio(w, err)
		return
	}
	compra, err := s.catalog.MiCompra()
	if err != nil {
		writeErr(w, err)
		return
	}
	s.escribirMiCompra(w, http.StatusOK, compra)
}

// DELETE /api/mi-compra
//
// Con cadena y url quita ese producto; sin ellos, vacía la lista entera.
func (s *Server) quitarDeLaCompra(w http.ResponseWriter, r *http.Request) {
	cadena := r.URL.Query().Get("cadena")
	rawURL := r.URL.Query().Get("url")

	if cadena == "" && rawURL == "" {
		if err := s.catalog.VaciarLaCompra(); err != nil {
			writeErr(w, err)
			return
		}
		writeBorrado(w)
		return
	}
	if !exigirProducto(w, cadena, rawURL) {
		return
	}
	if err := s.catalog.QuitarDeLaCompra(cadena, rawURL); err != nil {
		writeErr(w, err)
		return
	}
	writeBorrado(w)
}

func (s *Server) escribirMiCompra(w http.ResponseWriter, code int, compra catalog.MiCompraDTO) {
	writeJSON(w, code, respuestaMiCompra{
		MiCompraDTO: compra,
		AvisoTotal:  avisoTotal(compra),
	})
}

// avisoTotal explica, en una línea, por qué hay líneas fuera del total. Sin
// precio de unidad no hay total que valga: de un €/kg no se puede saber cuánto
// cuesta una bolsa, y estimarlo sería inventarse el gasto. Vacío cuando la lista
// se suma entera, para no meter ruido.
func avisoTotal(compra catalog.MiCompraDTO) string {
	if compra.SinPrecio == 0 {
		return ""
	}
	linea := "líneas"
	if compra.SinPrecio == 1 {
		linea = "línea"
	}
	// Dice las dos razones por las que una línea se queda fuera, que no es solo
	// una: lo vendido al peso no tiene precio de unidad porque la tienda solo
	// publica el €/kg, y una ficha sin precio (Alcampo, detrás de su WAF) no lo
	// tiene porque nadie lo ha escrito todavía.
	return fmt.Sprintf(
		"El total no incluye %d %s por no tener precio de unidad: o la tienda solo "+
			"publica el precio por medida (€/kg o €/l), que no dice cuánto cuesta "+
			"una bolsa, o todavía no hay precio para esa ficha. Están en "+
			"sinPrecioDetalle, sin estimar nada.",
		compra.SinPrecio, linea)
}
