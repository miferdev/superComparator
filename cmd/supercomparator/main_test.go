package main

import (
	"path/filepath"
	"testing"

	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/store"
)

func TestSelectChains(t *testing.T) {
	todos, err := selectChains(config.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeChains(todos)
	if len(todos) != 4 {
		t.Fatalf("cadenas por defecto = %d, want 4", len(todos))
	}
	ids := make([]string, 0, len(todos))
	for _, ch := range todos {
		ids = append(ids, ch.ID())
	}
	want := "mercadona,ahorramas,dia,alcampo"
	got := ""
	for i, id := range ids {
		if i > 0 {
			got += ","
		}
		got += id
	}
	if got != want {
		t.Errorf("cadenas = %s, want %s", got, want)
	}
}

func TestSelectChainsDesconocida(t *testing.T) {
	chains, err := selectChains(config.Config{}, []string{"carrefour"})
	if err == nil {
		closeChains(chains)
		t.Fatal("una cadena desconocida debe dar error")
	}
}

// TestSeedCatalogNoPisaLoAjustado: si alguien cambia el ritmo o activa los
// precios de Alcampo en la base, volver a sembrar no debe deshacerlo.
func TestSeedCatalogNoPisaLoAjustado(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := seedCatalog(st); err != nil {
		t.Fatal(err)
	}
	// Ajuste manual: activar los precios de Alcampo y pausar Mercadona.
	if _, err := st.DB().Exec(`UPDATE chains SET precios_activos = 1, pausa_segundos = 9 WHERE id = 'alcampo'`); err != nil {
		t.Fatal(err)
	}
	if err := seedCatalog(st); err != nil {
		t.Fatal(err)
	}
	ch, ok, err := st.Chain("alcampo")
	if err != nil || !ok {
		t.Fatal("alcampo debería seguir en chains")
	}
	if !ch.PreciosActivos || ch.PausaSegundos != 9 {
		t.Errorf("el ajuste se ha perdido: %+v", ch)
	}
}

func TestLoadConfigAplicaFlags(t *testing.T) {
	f := flags{addr: "0.0.0.0:9000", db: "/tmp/x.db", chains: "mercadona,dia"}
	cfg := loadConfig(&f)
	if cfg.Addr != "0.0.0.0:9000" {
		t.Errorf("addr = %q", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/x.db" {
		t.Errorf("db = %q", cfg.DBPath)
	}
	if len(cfg.Chains) != 2 || cfg.Chains[1] != "dia" {
		t.Errorf("cadenas = %v", cfg.Chains)
	}
}

func TestConfigPorDefecto(t *testing.T) {
	t.Setenv("SUPERCOMPARATOR_ADDR", "")
	t.Setenv("SUPERCOMPARATOR_DB", "")
	cfg := config.Load()
	if cfg.Addr != config.DefaultAddr {
		t.Errorf("addr = %q, want %q", cfg.Addr, config.DefaultAddr)
	}
	if cfg.DBPath == "" {
		t.Error("la base de datos debería tener ruta por defecto")
	}
}
