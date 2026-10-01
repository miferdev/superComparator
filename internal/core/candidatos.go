package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/match"
)

// candidato es una ficha descargada con el veredicto de la coincidencia.
// La resolución y el comando explain construyen estos mismos valores, así que
// lo que uno enseña es exactamente lo que el otro aplica.
type candidato struct {
	product  chain.Product
	score    float64
	motivos  []string
	aceptado bool
}

// descargarCandidatos pide a la cadena los candidatos de la consulta y
// descarga las fichas hasta el tope configurado, respetando la pausa.
func (c *Core) descargarCandidatos(ctx context.Context, query, chainID string) ([]chain.Product, error) {
	ch, ok := c.chains[chainID]
	if !ok {
		return nil, fmt.Errorf("cadena desconocida: %s", chainID)
	}
	ranked, err := c.candidates(ctx, query, chainID)
	if err != nil {
		return nil, err
	}
	if len(ranked) == 0 {
		return nil, errors.New("sin candidatos en el catálogo")
	}
	pool, maxCandidates := c.candidateLimits()
	var productos []chain.Product
	fetched := 0
	for _, cand := range ranked {
		if len(productos) >= maxCandidates || fetched >= pool {
			break
		}
		if cand.Score < c.cfg.MinScore {
			continue
		}
		if fetched > 0 {
			time.Sleep(c.cfg.Delay)
		}
		fetched++
		p, err := ch.Fetch(ctx, cand.Entry.URL)
		if err != nil {
			c.log.Debug("candidato descartado", "url", cand.Entry.URL, "err", err)
			continue
		}
		if !p.Available || p.Price <= 0 {
			continue
		}
		productos = append(productos, p)
	}
	return productos, nil
}

// evaluarCandidatos puntúa cada ficha contra lo pedido. Devuelve los candidatos
// válidos (los que ni una palabra tienen en común) y la lista de alternativas
// para el informe, ordenada de mayor a menor coincidencia.
func evaluarCandidatos(query string, productos []chain.Product) ([]candidato, []Alternative) {
	var candidatos []candidato
	alternativas := make([]Alternative, 0, len(productos))
	for _, p := range productos {
		v := match.Evaluar(query, p.Name, p.Format)
		if v.Score <= 0 {
			// Ni una palabra en común: no es el producto buscado.
			continue
		}
		alternativas = append(alternativas, Alternative{
			URL: p.URL, Name: p.Name, Score: v.Score,
			Price: p.Price, MeasurePrice: p.MeasurePrice, MeasureUnit: p.MeasureUnit,
		})
		candidatos = append(candidatos, candidato{
			product: p, score: v.Score, motivos: v.Motivos, aceptado: v.Aceptado,
		})
	}
	sort.Slice(alternativas, func(i, j int) bool { return alternativas[i].Score > alternativas[j].Score })
	return candidatos, alternativas
}

// elegirCandidato devuelve el candidato con el que se resuelve el producto. Si
// hay varios por encima del umbral gana el más barato por precio absoluto (la
// misma regla que usa la comparativa); si ninguno lo supera, devuelve el mejor
// para que se pueda revisar a mano.
func elegirCandidato(candidatos []candidato) (candidato, bool) {
	if len(candidatos) == 0 {
		return candidato{}, false
	}
	mejor := candidatos[0]
	for _, c := range candidatos[1:] {
		if c.score > mejor.score {
			mejor = c
		}
	}
	aceptables := make([]comparable, 0, len(candidatos))
	indexes := make([]int, 0, len(candidatos))
	for i, c := range candidatos {
		if !c.aceptado {
			continue
		}
		aceptables = append(aceptables, comparableOfProduct(c.product))
		indexes = append(indexes, i)
	}
	if k, _ := cheapestIndex(aceptables); k >= 0 {
		return candidatos[indexes[k]], true
	}
	return mejor, false
}
