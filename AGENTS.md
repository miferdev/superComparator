# AGENTS.md — convenciones del proyecto

Proyecto hecho con opencode + IA. Este fichero guía a cualquier agente que
trabaje en el repositorio. La arquitectura detallada está en
[ARCHITECTURE.md](ARCHITECTURE.md) y el uso en [README.md](README.md).

## Qué es

Comparador de precios de la compra entre **Mercadona** y **Ahorramas**:
lee `lista.md` (tabla markdown de 2 columnas), resuelve cada producto, compara
precios por unidad y por peso (€/kg, €/L) y escribe un informe markdown
(`datos/informe.md`) con la opción más barata de cada supermercado y un enlace
directo a la ficha. Es un programa de línea de comandos: **no hay TUI**.

## Stack

- Go 1.27.1, módulo `github.com/miferdev/superComparator`.
- Scraping: `go-rod/rod` (Mercadona, headless) y `net/http` + JSON-LD (Ahorramas).
- Persistencia: SQLite puro Go (`modernc.org/sqlite`), sin cgo.
- CLI: `cobra`. Parseo: `goquery`, `encoding/json`, `encoding/xml`.
- Salida: `internal/report` genera markdown y texto de consola. **Textos en español.**

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
make run               # flujo completo en local
make check             # alias de run
make up                # docker compose up
```

- Un solo test: `go test ./internal/match -run TestRank`.
- Los tests de red viven en `internal/chain/*/integration_test.go` con
  `//go:build integration`; requieren red real y Chromium.
- Fixtures HTML reales en `testdata/` (una ficha por cadena).

## Ejecución

- Docker: **`docker compose up`** (o `docker compose run --rm app`). El
  proceso es un lote: lee la lista, resuelve, comprueba, escribe el informe,
  imprime la comparativa y termina. `compose.yml` monta el repo en `/compras`
  y `./datos` en `/datos`.
- Sin Docker hace falta Chromium/Chrome en el `PATH`; si no,
  `SUPERCOMPARATOR_BROWSER_BIN=/ruta/a/chrome`. En Docker se usa
  `ROD_BROWSER_BIN=/headless-shell/headless-shell`.
- Subcomandos: `report` (regenera el informe desde la base), `smoke`
  (diagnóstico de una ficha por cadena) y `version`. Sin subcomando hace el
  flujo completo. `check` está obsoleto.
- Flags: `--lista`, `--cp`, `--db`, `--report`, `--browser-bin`, `--workers`,
  `--candidates`, `--delay-ms`.
- Config por env (tabla completa: `internal/config/config.go` y README). Las
  flags sobreescriben el env.

## Arquitectura (regla de dependencias)

`cmd → core → chain|match|store → config`, con `report` colgando de `core`.
Un solo sentido, sin ciclos:

- `list` y `match` son puros (sin red ni BD): testéalos con fixtures.
- `core` usa `chain`, `match` y `store`.
- Los adaptadores de cadena solo importan `chain` y `config`. La interfaz
  `Chain` (`ID`, `Sitemap`, `Fetch`) está en `internal/chain/chain.go`.
- Un concepto por fichero; evitar ficheros de más de ~300 líneas. Nada de
  paquetes `utils`, `common` o `helpers`.

## Reglas de scraping (importante)

- Solo rutas permitidas por el `robots.txt` de cada cadena:
  - Mercadona: `/sitemap.xml` y `/product/...`. **Nunca `/api`.**
  - Ahorramas: sitemaps y fichas de producto. **Nunca `/buscador`, `/Search-ShowAjax` ni `/Product-Variation`.**
- La resolución se hace contra los sitemaps, no descargando el catálogo completo.
- Mercadona requiere fijar **CP 28032** una vez por sesión (cookie).
- User-Agent honesto con enlace al repo en las peticiones HTTP simples; nunca
  imitar el de un navegador.
- Concurrencia máxima 3, pausas (~300 ms) y backoff exponencial ante errores.

## Estructura

```
cmd/supercomparator/   # entrada CLI (flujo completo, report, smoke, version)
internal/config/       # env + flags
internal/list/         # parser de lista.md
internal/chain/        # interfaz Chain + mercadona/ + ahorramas/
internal/match/        # normalización, stemming ES y ranking
internal/store/        # SQLite (historial, matches)
internal/core/         # orquestador + eventos
internal/report/       # informe.md y salida de consola
```

`core.Resolve` puntúa candidatos con `match.Rank` (`cfg.RankPool`, 15 por
defecto), descarga hasta `cfg.Candidates` (6) y elige por `match.Similarity`
(nombre + formato; penaliza packs y medidas distintas). Entre los candidatos
que superan `match.AutoThreshold` gana **el más barato** (por €/kg o €/L si hay
medida, si no por precio total). `core.Check` revisita las URLs vinculadas, marca
disponibilidad y guarda historial (`core/events.go` define los eventos).

## Formato de `lista.md`

| Producto        | Cantidad |
| --------------- | -------- |
| Leche entera 1L | 3        |
| Pan de molde    |          |

Cantidad vacía o sin número = 1 unidad. Columnas extra se ignoran.
`lista.md` está en la raíz y se ignora en git; hay ejemplo en `lista.ejemplo.md`.

## Convenciones de código

- Identificadores en inglés; salida, docs y commits en español.
- Sin comentarios salvo que aporten intención.
- `gofmt`, `go vet ./...` y `go test ./...` deben pasar (CI).
- No commitear `lista.md`, `datos/` ni artefactos locales (ver `.gitignore`).