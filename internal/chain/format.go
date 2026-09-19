package chain

import "regexp"

var nameSizeRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(kg|kilos?|g|gramos?|l|litros?|ml|cl)\b`)

// FormatFromName extrae la medida del nombre de un producto ("400 g", "1,5 L").
func FormatFromName(name string) string {
	m := nameSizeRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[1] + " " + m[2]
}
