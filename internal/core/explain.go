package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/match"
)

// CandidatoExplicado es una ficha que el comando explain enseña con el
// veredicto de la coincidencia y el motivo del rechazo.
type CandidatoExplicado struct {
	Nombre   string
	URL      string
	Precio   float64
	Score    float64
	Aceptado bool
	Motivos  []string
	Elegido  bool
}

// CadenaExplicada es la traza de un producto en una cadena.
type CadenaExplicada struct {
	Chain      string
	Candidatos []CandidatoExplicado
	Nota       string
}

// Explicacion es la traza de un producto de la lista en todas las cadenas
// activas: qué se buscó, qué se encontró y por qué se aceptó o se descartó.
type Explicacion struct {
	Item    string
	Cadenas []CadenaExplicada
}

// Explain recorre la lista como lo haría la resolución, pero guardando el
// veredicto de cada candidato para poder enseñarlo. No escribe nada en la base
// de datos: es una herramienta de diagnóstico.
func (c *Core) Explain(ctx context.Context, items []list.Item) ([]Explicacion, error) {
	sem := make(chan struct{}, c.cfg.Workers)
	var wg sync.WaitGroup
	resultado := make([]Explicacion, len(items))
	errs := make([]error, len(items))

	for idx, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, item list.Item) {
			defer wg.Done()
			defer func() { <-sem }()
			resultado[idx], errs[idx] = c.explainItem(ctx, item)
		}(idx, item)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return resultado, nil
}

func (c *Core) explainItem(ctx context.Context, item list.Item) (Explicacion, error) {
	exp := Explicacion{Item: item.Name}
	for _, chainID := range c.order {
		explicada := CadenaExplicada{Chain: chainID}
		descargados, err := c.descargarCandidatos(ctx, item.Name, chainID)
		if err != nil {
			explicada.Nota = err.Error()
			exp.Cadenas = append(exp.Cadenas, explicada)
			continue
		}
		candidatos, _ := evaluarCandidatos(item.Name, descargados)
		if len(candidatos) == 0 {
			explicada.Nota = "ninguna ficha encontrada encaja con lo pedido"
			exp.Cadenas = append(exp.Cadenas, explicada)
			continue
		}
		mejor, ok := elegirCandidato(candidatos)
		for _, cand := range candidatos {
			explicada.Candidatos = append(explicada.Candidatos, CandidatoExplicado{
				Nombre:   cand.product.Name,
				URL:      cand.product.URL,
				Precio:   cand.product.Price,
				Score:    cand.score,
				Aceptado: cand.aceptado,
				Motivos:  cand.motivos,
				Elegido:  ok && cand.product.URL == mejor.product.URL,
			})
		}
		if !ok {
			explicada.Nota = fmt.Sprintf("ninguno llega al umbral de %.2f: el mejor es %.2f",
				match.AutoThreshold, mejor.score)
		}
		exp.Cadenas = append(exp.Cadenas, explicada)
	}
	return exp, nil
}
