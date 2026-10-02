package store

import (
	"sync"
	"time"
)

// Pausas del bucle de precios: cuánto se espera entre dos descargas de la misma
// cadena, con la pausa que dice su configuración y con el backoff de los
// reintentos. Van por flujo, así que una cadena con concurrencia > 1 golpea en
// paralelo sin salirse de su ritmo, y dos cadenas distintas nunca se esperan
// entre sí.
const (
	// backoffBase es la primera espera del reintento; luego dobla por cada
	// intento hasta backoffCap.
	backoffBase = 30 * time.Second
	backoffCap  = 5 * time.Minute
)

// backoff dobla la espera por cada intento: 30s, 60s, 120s, 240s y a partir de
// ahí el tope de 5 minutos.
func (l *PriceLoop) backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := backoffBase << uint(attempts-1)
	if d <= 0 || d > backoffCap {
		return backoffCap
	}
	return d
}

// ritmo lleva el próximo hueco permitido de cada flujo de una cadena, para que
// la pausa se respete también entre tandas del bucle y no solo dentro de una.
type ritmo struct {
	mu   sync.Mutex
	prox map[string][]time.Time
}

// toma devuelve cuánto tiene que dormir el flujo antes de golpear la cadena y
// anota su próximo hueco.
func (r *ritmo) toma(c Chain, flujo int, now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.prox == nil {
		r.prox = map[string][]time.Time{}
	}
	prox := r.prox[c.ID]
	for len(prox) <= flujo {
		prox = append(prox, time.Time{})
	}
	// El hueco se ancla en el momento en que se toca la cadena y no en el
	// previsto: si el worker se retrasa, no se acumulan descargas seguidas.
	desde := now
	if prox[flujo].After(desde) {
		desde = prox[flujo]
	}
	prox[flujo] = desde.Add(time.Duration(c.PausaSegundos * float64(time.Second)))
	r.prox[c.ID] = prox
	return desde.Sub(now)
}
