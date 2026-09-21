package tui

import (
	"strings"
	"testing"
)

func TestBigTextFilasUniformes(t *testing.T) {
	rows := bigText("12,34")
	if len(rows) != 5 {
		t.Fatalf("filas = %d", len(rows))
	}
	for i, row := range rows {
		if strings.TrimSpace(row) == "" {
			t.Fatalf("fila %d vacía", i)
		}
		if len([]rune(row)) != len([]rune(rows[0])) {
			t.Fatalf("fila %d de ancho distinto: %q vs %q", i, row, rows[0])
		}
	}
}

func TestBigMoneyConEuro(t *testing.T) {
	if got := bigMoney(12.5); !strings.Contains(got, "€") {
		t.Fatalf("falta el símbolo €: %q", got)
	}
}
