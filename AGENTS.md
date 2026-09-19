# AGENTS.md — convenciones del proyecto

Proyecto hecho con opencode + IA. Este fichero guía a cualquier agente que
trabaje en el repositorio.

## Qué es

Comparador de precios de la compra entre **Mercadona** y **Ahorramas**:
lee `lista.md` (tabla markdown de 2 columnas), resuelve cada producto y compara
precios por unidad y por peso (€/kg, €/L), con historial y detección de
descatalogados.

## Stack

- Go 1.27, módulo `github.com/miferdev/superComparator`.
- TUI: Bubble Tea v2 + Bubbles + Lip Gloss + Glamour. **Textos en español.**
- Scraping: `go-rod/rod` (Mercadona, headless) y `net/http` + JSON-LD (Ahorramas).
- Persistencia: SQLite puro Go (`modernc.org/sqlite`), sin cgo.
- CLI: `cobra`. Parseo: `goquery`, `encoding/json`, `encoding/xml`.

## Reglas de scraping (importante)

- Solo rutas permitidas por el `robots.txt` de cada cadena:
  - Mercadona: `/sitemap.xml` y `/product/...`. **Nunca `/api`.**
  - Ahorramas: sitemaps y fichas de producto. **Nunca `/buscador`, `/Search-ShowAjax` ni `/Product-Variation`.**
- La resolución se hace contra los sitemaps, no descargando el catálogo completo.
- Mercadona requiere fijar **CP 28032** una vez por sesión (cookie).
- Concurrencia máxima 3, pausas (~300 ms) y backoff exponencial ante errores.

## Estructura

```
cmd/supercomparator/   # entrada CLI (tui por defecto, check, report)
internal/config/       # env + flags
internal/list/         # parser de lista.md
internal/chain/        # interfaz Chain + mercadona/ + ahorramas/
internal/match/        # normalización, stemming ES y ranking
internal/store/        # SQLite (historial, matches)
internal/core/         # orquestador + eventos para la TUI
internal/report/       # informe.md
internal/tui/          # Bubble Tea
```

## Formato de `lista.md`

| Producto        | Cantidad |
| --------------- | -------- |
| Leche entera 1L | 3        |
| Pan de molde    |          |

Cantidad vacía o sin número = 1 unidad. Columnas extra se ignoran.

## Convenciones de código

- Identificadores en inglés; UI, docs y commits en español.
- Sin comentarios salvo que aporten intención.
- `gofmt`, `go vet ./...` y `go test ./...` deben pasar (CI).
- Tests con fixtures reales en `testdata/`; los tests de red van con build tag
  `integration` y no corren en CI.
- No commitear `lista.md`, `datos/` ni artefactos locales (ver `.gitignore`).
