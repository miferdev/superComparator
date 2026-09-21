# AGENTS.md — convenciones del proyecto

Proyecto hecho con opencode + IA. Este fichero guía a cualquier agente que
trabaje en el repositorio. La arquitectura detallada está en
[ARCHITECTURE.md](ARCHITECTURE.md) y el uso en [README.md](README.md).

## Qué es

Comparador de precios de la compra entre **Mercadona** y **Ahorramas**:
lee `lista.md` (tabla markdown de 2 columnas), resuelve cada producto y compara
precios por unidad y por peso (€/kg, €/L), con historial y detección de
descatalogados.

## Stack

- Go 1.27.1, módulo `github.com/miferdev/superComparator`.
- TUI: Bubble Tea v2 + Bubbles + Lip Gloss + Glamour. **Textos en español.**
  Ojo con los imports: son `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
  `charm.land/lipgloss/v2` (no `github.com/charmbracelet/...`).
- Scraping: `go-rod/rod` (Mercadona, headless) y `net/http` + JSON-LD (Ahorramas).
- Persistencia: SQLite puro Go (`modernc.org/sqlite`), sin cgo.
- CLI: `cobra`. Parseo: `goquery`, `encoding/json`, `encoding/xml`.

## Comandos

Orden de verificación que corre CI (`.github/workflows/ci.yml`):

```sh
test -z "$(gofmt -l .)"   # formato
go vet ./...
go test ./...
go build ./...
```

Atajos del `Makefile`:

```sh
make test              # tests unitarios
make test-integration  # tests de red (build tag integration), no corren en CI
make build             # bin/  ->  bin/supercomparator
make run               # TUI en local
make check             # resolución + comprobación sin TUI (cron)
make up                # docker compose run --rm app
```

- Un solo test: `go test ./internal/match -run TestRank`.
- Los tests de red viven en `internal/chain/*/integration_test.go` con
  `//go:build integration`; requieren red real y Chromium.
- Fixtures HTML reales en `testdata/` (una ficha por cadena).

## Ejecución

- TUI en Docker: **`docker compose run --rm app`**, nunca `docker compose up`
  (no adjunta stdin y la TUI no recibe teclado). `compose.yml` monta el repo en
  `/compras` y `./datos` en `/datos`.
- En local hace falta Chromium/Chrome en el `PATH`; si no,
  `SUPERCOMPARATOR_BROWSER_BIN=/ruta/a/chrome`. En Docker se usa
  `ROD_BROWSER_BIN=/headless-shell/headless-shell`.
- Subcomandos: `check`, `report`, `smoke` (diagnóstico de una ficha por cadena),
  `version`. Sin subcomando arranca la TUI.
- Config por env (tabla completa: `internal/config/config.go` y README). Las
  flags de `cmd/supercomparator/main.go` sobreescriben el env.

## Arquitectura (regla de dependencias)

`cmd → tui/core → chain|match|store → config`. Un solo sentido, sin ciclos:

- `list` y `match` son puros (sin red ni BD): testéalos con fixtures.
- `core` usa `chain`, `match` y `store`; **no** conoce `bubbletea`.
- `tui` solo conoce `core`; **nunca** importa `mercadona` ni `sqlite`.
- Los adaptadores de cadena solo importan `chain`. La interfaz `Chain`
  (`ID`, `Sitemap`, `Fetch`) está en `internal/chain/chain.go`.
- Un concepto por fichero; evitar ficheros de más de ~300 líneas. Nada de
  paquetes `utils`, `common` o `helpers`.

## Reglas de scraping (importante)

- Solo rutas permitidas por el `robots.txt` de cada cadena:
  - Mercadona: `/sitemap.xml` y `/product/...`. **Nunca `/api`.**
  - Ahorramas: sitemaps y fichas de producto. **Nunca `/buscador`, `/Search-ShowAjax` ni `/Product-Variation`.**
- La resolución se hace contra los sitemaps, no descargando el catálogo completo.
- Mercadona requiere fijar **CP 28032** una vez por sesión (cookie).
- Concurrencia máxima 3, pausas (~300 ms) y backoff exponencial ante errores.

## Estructura

```
cmd/supercomparator/   # entrada CLI (tui por defecto, check, report, smoke, version)
internal/config/       # env + flags
internal/list/         # parser de lista.md
internal/chain/        # interfaz Chain + mercadona/ + ahorramas/
internal/match/        # normalización, stemming ES y ranking
internal/store/        # SQLite (historial, matches)
internal/core/         # orquestador + eventos para la TUI
internal/report/       # informe.md
internal/tui/          # Bubble Tea (selector de listas: solo carpetas y .md)
```

`core.Resolve` puntúa candidatos con `match.Rank` (`cfg.RankPool`, 15 por
defecto), descarga hasta `cfg.Candidates` (6) y elige por `match.Similarity`
(nombre + formato; penaliza packs y medidas distintas). El ganador se decide con
`cheapestIndex` (`core/price_compare.go`): solo compara €/kg o €/l entre medidas
de la misma dimensión y, si no, por precio total; descarta agotados y precios
caducados (`cfg.MaxPriceAge`). `core.Check` revisita las URLs vinculadas, marca
disponibilidad y guarda historial. La TUI se apoya en `core/events.go`.

## Formato de `lista.md`

| Producto        | Cantidad |
| --------------- | -------- |
| Leche entera 1L | 3        |
| Pan de molde    |          |

Cantidad vacía o sin número = 1 unidad. Columnas extra se ignoran.
`lista.md` está en la raíz y se ignora en git; hay ejemplo en `lista.ejemplo.md`.

## Convenciones de código

- Identificadores en inglés; UI, docs y commits en español.
- Sin comentarios salvo que aporten intención.
- `gofmt`, `go vet ./...` y `go test ./...` deben pasar (CI).
- No commitear `lista.md`, `datos/` ni artefactos locales (ver `.gitignore`).
