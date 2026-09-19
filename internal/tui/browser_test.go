package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserOnlyDirsAndMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"lista.md":   "| a | b |\n",
		"notas.txt":  "hola",
		".oculto.md": "x",
		"README.MD":  "x",
		"datos.json": "{}",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	b := newBrowser(dir)
	if len(b.entries) != 3 {
		t.Fatalf("entradas = %+v, se esperaban 3 (sub, lista.md, README.MD)", b.entries)
	}
	if !b.entries[0].dir || b.entries[0].name != "sub" {
		t.Fatalf("primera entrada = %+v, se esperaba el directorio", b.entries[0])
	}
	names := []string{b.entries[1].name, b.entries[2].name}
	if names[0] != "lista.md" || names[1] != "README.MD" {
		t.Fatalf("ficheros = %v", names)
	}
}

func TestBrowserSelectFileAndMove(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lista.md"), []byte("| a | b |\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := newBrowser(dir)
	if e, _ := b.selected(); !e.dir {
		t.Fatalf("se esperaba empezar en el directorio, hay %+v", e)
	}
	b.move(1)
	if e, _ := b.selected(); e.name != "lista.md" {
		t.Fatalf("seleccionado = %+v", e)
	}
	b.move(10)
	if e, _ := b.selected(); e.name != "lista.md" {
		t.Fatalf("el cursor se salió: %+v", e)
	}
	path, ok := b.enter()
	if !ok || path != filepath.Join(dir, "lista.md") {
		t.Fatalf("enter = %q, %v", path, ok)
	}
}

func TestBrowserEnterDirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "otra.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := newBrowser(dir)
	if _, ok := b.enter(); ok {
		t.Fatal("un directorio no debe devolverse como fichero elegido")
	}
	if b.dir != sub {
		t.Fatalf("dir = %s, se esperaba %s", b.dir, sub)
	}
	if len(b.entries) != 1 || b.entries[0].name != "otra.md" {
		t.Fatalf("entradas = %+v", b.entries)
	}
	b.up()
	if b.dir != dir {
		t.Fatalf("tras subir, dir = %s", b.dir)
	}
}
