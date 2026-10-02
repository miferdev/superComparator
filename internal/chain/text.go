package chain

import "html"

// UnescapeText desescapa el texto que viene de una fuente que el parser de HTML
// no ha interpretado: el contenido de un <script type="application/ld+json"> es
// texto plano para net/html, así que un "Hellmann&#039;s" llega entero a
// products.name y el usuario no encuentra el producto buscando el apóstrofo.
//
// Solo para eso. El texto que sale del DOM (goquery .Text()) ya viene
// desescapado por el parser de HTML y volver a desescaparlo deshace un
// "&amp;lt;" que en la ficha quería decir "&lt;" y acaba mostrando "<". Y nunca
// para precios ni medidas: esos son números, no texto.
//
// Los espacios duros que deja "&nbsp;" se vuelven espacios normales: un U+00A0
// en un nombre no es lo que se quiere guardar ni comparar.
func UnescapeText(s string) string {
	return NormalizeSpaces(html.UnescapeString(s))
}
