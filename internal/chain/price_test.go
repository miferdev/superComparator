package chain

import (
	"encoding/json"
	"testing"
)

func TestParseJSONNumber(t *testing.T) {
	casos := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1.24", 1.24, true},
		{"1234.56", 1234.56, true},
		{"1,24", 1.24, true},
		{`"1.24"`, 1.24, true},
		{"", 0, false},
		{"nada", 0, false},
	}
	for _, c := range casos {
		got, ok := ParseJSONNumber(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseJSONNumber(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestPreciosNoSonIntercambiables fija qué formato lee cada función. No son
// intercambiables y confundirlas no da error: devuelve un número plausible pero
// equivocado, que es justo lo que pasa con el precio.
//
// ParsePrice es para el texto en español que enseña la web ("1.234,56 €") y
// ParseJSONNumber para el número de máquina del JSON-LD ("1234.56"), que es lo
// que publica schema.org. La columna "precioEspanol" deja escrito lo que sale
// cuando se usa la que no toca: "1.65" da 1, "1234.56" da 123 y "0.99" no da
// nada, sin ningún aviso. Ese fue el bug de Ahorramas: un 40 % del precio
// perdido en silencio, y los productos de menos de un euro sin precio. Si algún
// día se cambia ParsePrice para que también acepte el punto, este test obliga a
// decidir a propósito qué función se usa en cada sitio.
func TestPreciosNoSonIntercambiables(t *testing.T) {
	casos := []struct {
		texto string
		// Lo que quiere cada función con su propio formato.
		precioEspanol float64
		okEspanol     bool
		precioMaquina float64
		okMaquina     bool
	}{
		// Formato español: solo lo entiende ParsePrice.
		{"1.234,56 €", 1234.56, true, 0, false},
		{"1,65 €", 1.65, true, 0, false},
		{"1,00 €", 1.00, true, 0, false},
		// Formato máquina: solo lo entiende ParseJSONNumber.
		{"1.65", 1, true, 1.65, true},
		{"1234.56", 123, true, 1234.56, true},
		{"0.99", 0, false, 0.99, true},
	}
	for _, c := range casos {
		if f, ok := ParsePrice(c.texto); ok != c.okEspanol || (ok && f != c.precioEspanol) {
			t.Errorf("ParsePrice(%q) = %v,%v; want %v,%v", c.texto, f, ok, c.precioEspanol, c.okEspanol)
		}
		if f, ok := ParseJSONNumber(c.texto); ok != c.okMaquina || (ok && f != c.precioMaquina) {
			t.Errorf("ParseJSONNumber(%q) = %v,%v; want %v,%v", c.texto, f, ok, c.precioMaquina, c.okMaquina)
		}
	}
}

func TestJSONText(t *testing.T) {
	var v struct {
		Precio JSONText `json:"price"`
	}
	if err := json.Unmarshal([]byte(`{"price": 1.24}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Precio != "1.24" {
		t.Errorf("precio = %q, want 1.24", v.Precio)
	}
}
