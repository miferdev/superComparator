package chain

import "testing"

func TestUnescapeText(t *testing.T) {
	casos := []struct{ in, want string }{
		{"Salsa barbacoa Hellmann&#039;s 285 g", "Salsa barbacoa Hellmann's 285 g"},
		{"Pan &amp; Butter", "Pan & Butter"},
		{"Hellmann&#x27;s", "Hellmann's"},
		{"Caf&eacute; con&ntilde;e", "Café conñe"},
		{"Salsa Hellmann&#x27;s &amp; caf&eacute;", "Salsa Hellmann's & café"},
		{"Leche&nbsp;entera 1 L", "Leche entera 1 L"},
		{"100% &euro;", "100% €"},
		// Desescapar no es deshacer: un "&amp;lt;" de la ficha quiere decir
		// "&lt;" escrito, y un segundo paso lo convertiría en "<".
		{"Cacao &amp;lt; 100 g", "Cacao &lt; 100 g"},
		{"&amp;amp;", "&amp;"},
		// Una "&" que no abre entidad se queda como está.
		{"Pipes & Co", "Pipes & Co"},
		{"", ""},
	}
	for _, c := range casos {
		if got := UnescapeText(c.in); got != c.want {
			t.Errorf("UnescapeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
