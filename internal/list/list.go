// Package list parsea la lista de la compra, una tabla markdown de dos
// columnas: producto y cantidad (vacía o sin número = 1 unidad).
package list

import (
	"bufio"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Item struct {
	Name     string
	Quantity int
	Line     int
}

var (
	numberRe    = regexp.MustCompile(`\d+`)
	separatorRe = regexp.MustCompile(`^:?-{2,}:?$`)
)

func ParseFile(path string) ([]Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func Parse(r io.Reader) ([]Item, error) {
	var items []Item
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(text, "|") {
			continue
		}
		cells := splitRow(text)
		if len(cells) == 0 || isSeparator(cells) {
			continue
		}
		name := strings.TrimSpace(cells[0])
		if name == "" || strings.EqualFold(name, "producto") {
			continue
		}
		items = append(items, Item{Name: name, Quantity: quantity(cells), Line: line})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("la lista no contiene filas válidas")
	}
	return items, nil
}

func splitRow(text string) []string {
	text = strings.TrimPrefix(text, "|")
	text = strings.TrimSuffix(text, "|")
	raw := strings.Split(text, "|")
	cells := make([]string, 0, len(raw))
	for _, c := range raw {
		cells = append(cells, strings.TrimSpace(c))
	}
	return cells
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if !separatorRe.MatchString(strings.ReplaceAll(c, " ", "")) {
			return false
		}
	}
	return true
}

func quantity(cells []string) int {
	if len(cells) < 2 {
		return 1
	}
	m := numberRe.FindString(cells[1])
	if m == "" {
		return 1
	}
	n, err := strconv.Atoi(m)
	if err != nil || n < 1 {
		return 1
	}
	return n
}
