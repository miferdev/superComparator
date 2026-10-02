package chain

import "testing"

func TestCategoryPath(t *testing.T) {
	casos := []struct {
		niveles []string
		want    string
	}{
		{[]string{"Alimentación", "Arroces, Pastas y Legumbres", "Legumbres"}, "Alimentación / Arroces, Pastas y Legumbres / Legumbres"},
		{[]string{"Cacao, café e infusiones", "Cacao soluble y chocolate a la taza"}, "Cacao, café e infusiones / Cacao soluble y chocolate a la taza"},
		{[]string{"Frutas y verduras"}, "Frutas y verduras"},
		{[]string{"Alimentación", "", "Legumbres"}, "Alimentación / Legumbres"},
		{[]string{}, ""},
		{nil, ""},
	}
	for _, c := range casos {
		if got := CategoryPath(c.niveles...); got != c.want {
			t.Errorf("CategoryPath(%q) = %q, want %q", c.niveles, got, c.want)
		}
	}
}
