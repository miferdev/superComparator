package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Esto son errores de la lista de la compra, no fallos del servidor: son
// peticiones que no se pueden atender. store los devuelve ya en español, así que
// el status se decide mirando el mensaje. Lo que no aparece en ninguna de las
// dos listas se deja en 500 a propósito: un fallo de SQLite no es que el cliente
// haya pedido mal la cosa.
var (
	// 404: la cosa que se nombra no existe.
	noExiste = []string{
		"no está en el catálogo",
		"no está en la lista",
	}
	// 400: lo que se pide no tiene sentido o no cuadra con lo guardado.
	noValido = []string{
		"la cantidad tiene que ser",
		"ya está en la lista",
		"hace falta un precio de unidad",
		"necesita su medida",
		"la medida tiene que ser kg o l",
		"sin un precio por medida",
	}
)

// writeErrDominio traduce un error de la lista o de los precios manuales al
// status que le toca. Un error de SQLite que no sea de esos no lo disfraza: sale
// como 500, que es lo que es.
func writeErrDominio(w http.ResponseWriter, err error) {
	msg := err.Error()
	if contiene(msg, noExiste) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		return
	}
	if contiene(msg, noValido) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	writeErr(w, err)
}

func contiene(msg string, fallos []string) bool {
	for _, f := range fallos {
		if strings.Contains(msg, f) {
			return true
		}
	}
	return false
}

// writeBorrado responde a un borrado. No lleva cuerpo la respuesta de un DELETE:
// lo que la web quiere saber es que ya no está.
func writeBorrado(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]bool{"borrado": true})
}

// decodeJSON lee el cuerpo de una petición. Devuelve false cuando ya ha
// contestado con un 400, para que quien llama solo tenga que mirar el error.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	// 64 KiB de sobra para un cuerpo con una URL y una nota, y corta de golpe lo
	// que venga justo después de un `}`.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON no válido: " + err.Error()})
		return false
	}
	return true
}

// exigirProducto pide cadena y url. Sin las dos no hay producto del que hablar:
// quitar la lista entera no es lo mismo que quitar un producto, y no se decide
// por un parámetro que se ha olvidado.
func exigirProducto(w http.ResponseWriter, cadena, productURL string) bool {
	if cadena == "" || productURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hacen falta los parámetros cadena y url"})
		return false
	}
	return true
}
