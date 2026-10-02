package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ColaCadena es el estado de la cola de precios de una tienda, para la web.
type ColaCadena struct {
	Cadena      string `json:"cadena"`
	Nombre      string `json:"nombre"`
	Pendiente   int    `json:"pendiente"`
	Descargando int    `json:"descargando"`
	Error       int    `json:"error"`
	Precios     int    `json:"precios"`
}

// EstadoCola es lo que va por SSE: cuánto falta por poner precio y cuánto hay
// ya. Se lee de la base, que es donde vive la cola, así que lo que ve la web es
// lo que de verdad se ha descargado.
type EstadoCola struct {
	Cadenas []ColaCadena `json:"cadenas"`
	Cuándo  time.Time    `json:"cuando"`
}

// eventos manda el estado de la cola por Server-Sent Events. Es un stream: se
// abre una vez y cada poco llega una línea con el progreso, para que la web no
// tenga que preguntar. Al cerrar la pestaña se corta solo.
func (s *Server) eventos(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, fmt.Errorf("este servidor no puede mandar eventos en vivo"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// El primer evento va enseguida: si no, la web se queda en blanco esperando.
	s.enviarEstado(w, flusher, r)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			s.enviarEstado(w, flusher, r)
		}
	}
}

func (s *Server) enviarEstado(w http.ResponseWriter, f http.Flusher, r *http.Request) {
	estado := s.estadoCola(r)
	b, err := json.Marshal(estado)
	if err != nil {
		s.log.Debug("no se pudo marshalar el estado", "err", err)
		return
	}
	fmt.Fprintf(w, "event: estado\ndata: %s\n\n", b)
	f.Flush()
}

// estadoCola cuenta lo que queda por hacer en cada tienda. Una cadena que está
// en error es información, no un fallo: el resto sigue.
func (s *Server) estadoCola(r *http.Request) EstadoCola {
	estado := EstadoCola{Cuándo: time.Now().UTC(), Cadenas: []ColaCadena{}}
	cadenas, err := s.catalog.Chains(r.Context())
	if err != nil {
		return estado
	}
	for _, c := range cadenas {
		stats, err := s.store.QueueStats(c.ID)
		if err != nil {
			s.log.Debug("no se pudo leer la cola", "cadena", c.ID, "err", err)
			continue
		}
		estado.Cadenas = append(estado.Cadenas, ColaCadena{
			Cadena:      c.ID,
			Nombre:      c.Nombre,
			Pendiente:   stats["pendiente"],
			Descargando: stats["descargando"],
			Error:       stats["error"],
			Precios:     s.conPrecio(c.ID),
		})
	}
	return estado
}

// conPrecio cuenta los productos de una cadena que ya tienen precio.
func (s *Server) conPrecio(chainID string) int {
	n, err := s.store.ProductsWithPrice(chainID)
	if err != nil {
		s.log.Debug("no se pudieron contar los precios", "cadena", chainID, "err", err)
		return 0
	}
	return n
}
