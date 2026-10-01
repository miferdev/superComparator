// Package server expone el catálogo por HTTP: una API JSON para la web y, más
// adelante, el progreso en vivo. No habla con ninguna tienda; solo lee y escribe
// en la base de datos.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/miferdev/superComparator/internal/catalog"
	"github.com/miferdev/superComparator/internal/store"
	"github.com/miferdev/superComparator/internal/version"
)

// Server es el servidor HTTP del catálogo.
type Server struct {
	catalog *catalog.Catalog
	store   *store.Store
	log     *slog.Logger
	addr    string
}

func New(cat *catalog.Catalog, st *store.Store, log *slog.Logger, addr string) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{catalog: cat, store: st, log: log, addr: addr}
}

// Handler construye las rutas. Lo devuelvo aparte para poder probarlo con
// httptest.NewServer sin abrir un puerto.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/estado", s.estado)
	mux.HandleFunc("GET /api/cadenas", s.cadenas)
	mux.HandleFunc("GET /api/catalogo", s.catalogo)
	mux.HandleFunc("GET /api/producto", s.producto)
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": version.Stamp()})
	})
	mux.HandleFunc("/", s.raiz)
	return logRequests(s.log, mux)
}

// ListenAndServe levanta el servidor y lo apaga con el contexto.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("escuchando en %s: %w", s.addr, err)
	}
	s.log.Info("web en marcha", "url", "http://"+ln.Addr().String())
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) estado(w http.ResponseWriter, r *http.Request) {
	cadenas, err := s.catalog.Chains(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	var ultimo string
	if run, ok, err := s.store.LastRun(); err == nil && ok {
		ultimo = run.Kind + " " + run.Chain + " · " + run.Note
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       version.Stamp(),
		"cadenas":       cadenas,
		"ultimoTrabajo": ultimo,
	})
}

func (s *Server) cadenas(w http.ResponseWriter, r *http.Request) {
	cadenas, err := s.catalog.Chains(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cadenas)
}

func (s *Server) catalogo(w http.ResponseWriter, r *http.Request) {
	q := store.SearchQuery{
		Text:      r.URL.Query().Get("q"),
		Chain:     r.URL.Query().Get("cadena"),
		ConPrecio: boolParam(r, "conPrecio"),
		Orden:     r.URL.Query().Get("orden"),
		Offset:    intParam(r, "offset", 0),
		Limit:     intParam(r, "limite", 50),
	}
	q = catalog.NormalizeSearchQuery(q)

	productos, total, hayMas, err := s.catalog.Search(r.Context(), q)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"productos": productos,
		"total":     total,
		"hayMas":    hayMas,
	})
}

func (s *Server) producto(w http.ResponseWriter, r *http.Request) {
	chainID := r.URL.Query().Get("cadena")
	// La URL viene con acentos y eñes: r.URL.Query() ya la ha decodificado, así
	// que se busca tal cual.
	rawURL := r.URL.Query().Get("url")
	if chainID == "" || rawURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hacen falta los parámetros cadena y url"})
		return
	}
	p, ok, err := s.catalog.Product(r.Context(), chainID, rawURL)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "producto no encontrado"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) raiz(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no encontrado"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="es"><head><meta charset="utf-8"><title>SuperComparator</title></head>
<body style="font-family:system-ui;max-width:40rem;margin:4rem auto;line-height:1.6">
<h1>SuperComparator</h1>
<p>La API del catálogo está en marcha. La web todavía no está construida (fase 2).</p>
<ul>
<li><a href="/api/cadenas">/api/cadenas</a> — cadenas y cuánto catálogo hay</li>
<li><a href="/api/catalogo?q=leche">/api/catalogo?q=leche</a> — buscar productos</li>
<li><a href="/api/estado">/api/estado</a> — estado del sistema</li>
</ul>
<p>Versión: %s</p>
</body></html>`, version.Stamp())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func boolParam(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true" || v == "si"
}

func intParam(r *http.Request, name string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return v
}

func logRequests(log *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Debug("petición", "method", r.Method, "path", r.URL.Path, "duración", time.Since(start))
	})
}
