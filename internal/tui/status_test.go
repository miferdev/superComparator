package tui

import (
	"strings"
	"testing"

	"github.com/miferdev/superComparator/internal/core"
)

func TestStatusEmoji(t *testing.T) {
	if got := statusEmoji("resuelto"); !strings.Contains(got, "✅") {
		t.Fatalf("resuelto debería ser tick verde: %q", got)
	}
	for _, status := range []string{"pendiente", "revisar", "error", "cambio", "descatalogado"} {
		if got := statusEmoji(status); !strings.Contains(got, "❌") {
			t.Fatalf("%s debería ser equis roja: %q", status, got)
		}
	}
}

func newStatusModel() *Model {
	return &Model{
		phase:  "resolver",
		status: map[string]string{},
		notes:  map[string]string{},
		alts:   map[string]map[string][]core.Alternative{},
		scores: map[string]map[string]float64{},
		failed: map[string]map[string]bool{},
	}
}

func TestStatusRevisarAunqueElBuenoLlegueAntes(t *testing.T) {
	m := newStatusModel()
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "mercadona", Score: 0.75})
	if m.status["pan"] != "resuelto" {
		t.Fatalf("estado = %q", m.status["pan"])
	}
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "ahorramas", Score: 0.33})
	if m.status["pan"] != "revisar" {
		t.Fatalf("el match flojo debe marcar revisar: %q", m.status["pan"])
	}
}

func TestStatusIndependienteDelOrden(t *testing.T) {
	m := newStatusModel()
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "ahorramas", Score: 0.33})
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "mercadona", Score: 0.75})
	if m.status["pan"] != "revisar" {
		t.Fatalf("estado = %q", m.status["pan"])
	}
}

func TestStatusResueltoSiTodasVanBien(t *testing.T) {
	m := newStatusModel()
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "mercadona", Score: 0.75})
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "ahorramas", Score: 0.64})
	if m.status["pan"] != "resuelto" {
		t.Fatalf("estado = %q", m.status["pan"])
	}
}

func TestStatusErrorSiTodoFalla(t *testing.T) {
	m := newStatusModel()
	m.handleEvent(core.ItemFailed{Item: "pan", Chain: "mercadona", Err: "timeout"})
	if m.status["pan"] != "error" {
		t.Fatalf("estado = %q", m.status["pan"])
	}
}

func TestStatusRevisarSiUnaCadenaFalla(t *testing.T) {
	m := newStatusModel()
	m.handleEvent(core.ChainResolved{Item: "pan", Chain: "mercadona", Score: 0.75})
	m.handleEvent(core.ItemFailed{Item: "pan", Chain: "ahorramas", Err: "timeout"})
	if m.status["pan"] != "revisar" {
		t.Fatalf("estado = %q", m.status["pan"])
	}
}
