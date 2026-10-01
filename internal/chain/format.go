package chain

import "regexp"

var (
	nameSizeRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(kg|kilos?|g|gramos?|l|litros?|ml|cl)\b`)
	packRe     = regexp.MustCompile(`(?i)(\d+)\s*(?:x|\*)\s*(\d+(?:[.,]\d+)?)\s*(kg|kilos?|g|gramos?|l|litros?|ml|cl)\b`)
)

// FormatFromName extrae el formato del nombre de un producto. Si el nombre
// describe un pack ("pack 6 x 1 L") devuelve el formato completo con el
// multiplicador, para que la comparación de medidas no lo confunda con una
// unidad suelta.
func FormatFromName(name string) string {
	if m := packRe.FindStringSubmatch(name); m != nil {
		return m[1] + " x " + m[2] + " " + m[3]
	}
	m := nameSizeRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[1] + " " + m[2]
}
