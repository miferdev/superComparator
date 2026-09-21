package chain

import "testing"

func TestNormalizeMeasure(t *testing.T) {
	cases := []struct {
		value float64
		unit  string
		want  float64
		wantU string
		ok    bool
	}{
		{1, "kg", 1, "kg", true},
		{400, "g", 0.4, "kg", true},
		{1, "G", 0.001, "kg", true},
		{1.5, "L", 1.5, "l", true},
		{200, "ml", 0.2, "l", true},
		{50, "cl", 0.5, "l", true},
		{2, "litros", 2, "l", true},
		{3, "unidades", 3, "ud", true},
		{1, "foo", 0, "", false},
	}
	for _, c := range cases {
		m, ok := NormalizeMeasure(c.value, c.unit)
		if ok != c.ok || (ok && (m.Unit != c.wantU || m.Value != c.want)) {
			t.Errorf("NormalizeMeasure(%v, %q) = %+v, %v", c.value, c.unit, m, ok)
		}
	}
}

func TestParseMeasurePrice(t *testing.T) {
	cases := []struct {
		in    string
		value float64
		unit  string
		ok    bool
	}{
		{"2,45 €/L", 2.45, "l", true},
		{"2,50&euro;/KG.PESO ESC", 2.50, "kg", true},
		{"0.85 €/kg", 0.85, "kg", true},
		{"0,15&euro;/CL", 15, "l", true},
		{"1,20&euro;/ML", 1200, "l", true},
		{"3 €/kg", 3, "kg", true},
		{"3 €/ud", 0, "", false},
		{"1,85&euro;/ud y 2,50&euro;/KG", 2.50, "kg", true},
		{"1,85€ por Unidad", 0, "", false},
		{"sin medida", 0, "", false},
	}
	for _, c := range cases {
		m, ok := ParseMeasurePrice(c.in)
		if ok != c.ok || (ok && (m.Unit != c.unit || m.Value != c.value)) {
			t.Errorf("ParseMeasurePrice(%q) = %+v, %v; want %v %s", c.in, m, ok, c.value, c.unit)
		}
	}
}
