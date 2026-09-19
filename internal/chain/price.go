package chain

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	trailingIDRe = regexp.MustCompile(`-\d+$`)
	priceRe      = regexp.MustCompile(`(\d{1,3}(?:\.\d{3})*|\d+)(?:,(\d{1,2}))?`)
	unicodeSpRe  = regexp.MustCompile(`\s+`)
)

// ParsePrice convierte un precio en formato español ("1.234,56 €") a float64.
func ParsePrice(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	m := priceRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	integer := strings.ReplaceAll(m[1], ".", "")
	value := integer
	if m[2] != "" {
		value += "." + m[2]
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return f, true
}

// HumanizeSlug convierte un slug de URL en un nombre legible.
func HumanizeSlug(slug string) string {
	slug = strings.TrimSuffix(slug, ".html")
	slug = trailingIDRe.ReplaceAllString(slug, "")
	slug = strings.NewReplacer("-", " ", "_", " ", "/", " ").Replace(slug)
	return strings.TrimSpace(unicodeSpRe.ReplaceAllString(slug, " "))
}
