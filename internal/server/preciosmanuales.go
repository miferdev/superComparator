package server

import (
	"net/http"

	"github.com/miferdev/superComparator/internal/catalog"
)

// peticionPrecioManual es el precio que el usuario escribe para una ficha que
// él ha mirado. Es su dato, no el de la tienda: por eso va a su propio sitio y
// se guarda sin tocar lo que publica la web.
type peticionPrecioManual struct {
	Cadena       string  `json:"cadena"`
	URL          string  `json:"url"`
	Precio       float64 `json:"precio"`
	PrecioMedida float64 `json:"precioMedida"`
	Medida       string  `json:"medida"`
	Nota         string  `json:"nota"`
}

// GET /api/precios-manuales
//
// Sin parámetros, todos los precios puestos a mano. Con cadena y url, solo ese:
// una ficha es una consulta con los mismos parámetros que el resto de la API.
func (s *Server) preciosManuales(w http.ResponseWriter, r *http.Request) {
	cadena := r.URL.Query().Get("cadena")
	rawURL := r.URL.Query().Get("url")

	if cadena == "" && rawURL == "" {
		manuales, err := s.catalog.PreciosManuales()
		if err != nil {
			writeErr(w, err)
			return
		}
		// Una lista vacía es `[]` y no `null`: la web la recorre sin mirar el tipo.
		if manuales == nil {
			manuales = []catalog.PrecioManualDTO{}
		}
		writeJSON(w, http.StatusOK, manuales)
		return
	}
	if !exigirProducto(w, cadena, rawURL) {
		return
	}
	dto, ok, err := s.catalog.GetPrecioManual(cadena, rawURL)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "ese producto no tiene precio puesto a mano"})
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// PUT /api/precios-manuales
//
// Guarda el precio y lo devuelve ya resuelto (con el nombre del producto y la
// marca de tiempo), que es lo que la web pinta en la celda.
func (s *Server) guardarPrecioManual(w http.ResponseWriter, r *http.Request) {
	var p peticionPrecioManual
	if !decodeJSON(w, r, &p) {
		return
	}
	if !exigirProducto(w, p.Cadena, p.URL) {
		return
	}
	if err := s.catalog.SetPrecioManual(p.Cadena, p.URL, p.Precio, p.PrecioMedida, p.Medida, p.Nota); err != nil {
		writeErrDominio(w, err)
		return
	}
	dto, ok, err := s.catalog.GetPrecioManual(p.Cadena, p.URL)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		// No debería pasar: se acaba de guardar. Si pasa, no se inventa lo que se
		// acaba de escribir.
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "el precio se guardó pero no se ha podido leer"})
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// DELETE /api/precios-manuales
//
// Aquí sí hacen falta las dos cosas: quitar el precio a mano de una ficha es lo
// que se puede pedir, pero vaciarlos todos de un golpe con un DELETE sin
// parámetros es una manera fácil de perderlos sin querer. Que sea la web la que
// lo haga, una vez por cada ficha.
func (s *Server) borrarPrecioManual(w http.ResponseWriter, r *http.Request) {
	cadena := r.URL.Query().Get("cadena")
	rawURL := r.URL.Query().Get("url")
	if !exigirProducto(w, cadena, rawURL) {
		return
	}
	// Quitarlo dos veces no es un error: el DELETE es idempotente.
	if err := s.catalog.BorrarPrecioManual(cadena, rawURL); err != nil {
		writeErr(w, err)
		return
	}
	writeBorrado(w)
}
