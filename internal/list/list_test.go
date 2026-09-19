package list

import (
	"strings"
	"testing"
)

func TestParseTable(t *testing.T) {
	input := `# Lista de la compra

| Producto        | Cantidad |
| --------------- | -------- |
| Leche entera 1L | 3        |
| Pan de molde    |          |
| Champú          | 1        |
| Papel higiénico | 0        |
`
	items, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []Item{
		{Name: "Leche entera 1L", Quantity: 3, Line: 5},
		{Name: "Pan de molde", Quantity: 1, Line: 6},
		{Name: "Champú", Quantity: 1, Line: 7},
		{Name: "Papel higiénico", Quantity: 1, Line: 8},
	}
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d (%+v)", len(items), len(want), items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("items[%d] = %+v, want %+v", i, items[i], want[i])
		}
	}
}

func TestParseWithoutHeader(t *testing.T) {
	items, err := Parse(strings.NewReader("| Agua 1,5L | 6 |\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Agua 1,5L" || items[0].Quantity != 6 {
		t.Fatalf("items = %+v", items)
	}
}

func TestParseExtraColumns(t *testing.T) {
	items, err := Parse(strings.NewReader("| Papel | 2 | higiene |\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if items[0].Quantity != 2 {
		t.Fatalf("cantidad = %d, want 2", items[0].Quantity)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse(strings.NewReader("esto no es una tabla\n")); err == nil {
		t.Fatal("se esperaba error con una lista vacía")
	}
}
