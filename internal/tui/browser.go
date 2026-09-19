package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type fileEntry struct {
	name string
	dir  bool
}

// browser es un selector de ficheros mínimo: solo muestra directorios y
// ficheros markdown, que es lo único que necesita la aplicación.
type browser struct {
	dir     string
	entries []fileEntry
	cursor  int
	err     string
	height  int
}

func newBrowser(dir string) browser {
	b := browser{dir: dir, height: 12}
	b.reload()
	return b
}

func (b *browser) reload() {
	b.entries = nil
	b.cursor = 0
	b.err = ""

	entries, err := os.ReadDir(b.dir)
	if err != nil {
		b.err = err.Error()
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			b.entries = append(b.entries, fileEntry{name: name, dir: true})
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".md") {
			b.entries = append(b.entries, fileEntry{name: name})
		}
	}
	sort.Slice(b.entries, func(i, j int) bool {
		if b.entries[i].dir != b.entries[j].dir {
			return b.entries[i].dir
		}
		return strings.ToLower(b.entries[i].name) < strings.ToLower(b.entries[j].name)
	})
}

func (b *browser) move(delta int) {
	if len(b.entries) == 0 {
		return
	}
	b.cursor += delta
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= len(b.entries) {
		b.cursor = len(b.entries) - 1
	}
}

func (b *browser) selected() (fileEntry, bool) {
	if b.cursor < 0 || b.cursor >= len(b.entries) {
		return fileEntry{}, false
	}
	return b.entries[b.cursor], true
}

func (b *browser) enter() (string, bool) {
	e, ok := b.selected()
	if !ok {
		return "", false
	}
	if e.dir {
		b.dir = filepath.Join(b.dir, e.name)
		b.reload()
		return "", false
	}
	return filepath.Join(b.dir, e.name), true
}

func (b *browser) up() {
	parent := filepath.Dir(b.dir)
	if parent == b.dir {
		return
	}
	b.dir = parent
	b.reload()
}

// update procesa una tecla y devuelve la ruta elegida, si la hay.
func (b *browser) update(msg tea.Msg) (string, bool) {
	key, ok := keyOf(msg)
	if !ok {
		return "", false
	}
	switch key {
	case "up", "k":
		b.move(-1)
	case "down", "j":
		b.move(1)
	case "pgup":
		b.move(-b.visible())
	case "pgdown":
		b.move(b.visible())
	case "enter":
		return b.enter()
	case "left", "h", "backspace":
		b.up()
	}
	return "", false
}

func (b *browser) visible() int {
	if b.height < 1 {
		return 1
	}
	return b.height
}

func (b *browser) view() string {
	var sb strings.Builder
	sb.WriteString(accentStyle.Render(b.dir))
	sb.WriteString("\n\n")
	if b.err != "" {
		sb.WriteString(errStyle.Render(b.err))
		return sb.String()
	}
	if len(b.entries) == 0 {
		sb.WriteString(dimStyle.Render("(sin carpetas ni ficheros .md)"))
		return sb.String()
	}

	start := 0
	if b.cursor >= b.visible() {
		start = b.cursor - b.visible() + 1
	}
	end := min(start+b.visible(), len(b.entries))
	for i := start; i < end; i++ {
		e := b.entries[i]
		label := e.name
		if e.dir {
			label += string(filepath.Separator)
		}
		switch {
		case i == b.cursor:
			sb.WriteString(selectedRowStyle.Render(" " + label + " "))
		case e.dir:
			sb.WriteString(accentStyle.Render(label))
		default:
			sb.WriteString(label)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
