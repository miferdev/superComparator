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
