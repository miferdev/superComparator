# Arquitectura

SuperComparator es un programa de línea de comandos que compara precios de la
compra entre Mercadona y Ahorramas y escribe el resultado en un fichero
markdown. Esta guía explica las piezas y el orden recomendado de lectura.

## Vista general

```
cmd/supercomparator/main.go     punto de composición y flujo por defecto (~80 líneas)
internal/
  config/    env + flags          no depende de nadie
  list/      parser de lista.md   puro, sin red ni BD
  match/     normalización/ranking puro, sin red ni BD
  chain/     interfaz (puerto) Chain + tipos Product/SitemapEntry
    mercadona/  rod headless (CP 28032, /sitemap.xml y /product)
    ahorramas/  HTTP + JSON-LD (sitemaps y fichas)
  store/     SQLite (items, matches, price_history)
  core/      orquestador + eventos; usa chain, match y store
  report/    informe.md y salida de consola (depende de los tipos de core)
```

Regla de dependencias: `cmd → core → chain|match|store → config`, con `report`
colgando de `core`. Las flechas van en un solo sentido; no hay ciclos. Los
adaptadores de cadena solo importan `chain` y `config`.

## Flujo de datos

1. `list` parsea la tabla markdown y devuelve `[]list.Item`.
2. `core.Resolve` pide el sitemap a cada `chain.Chain`, puntúa candidatos con
   `match.Rank`, descarga los mejores y elige por `match.Similarity`
   (nombre + formato; penaliza packs de tamaño distinto). Guarda el match y el
   precio en `store` y emite eventos (`ChainResolved`, `ItemNeedsReview`…).
3. `core.Check` revisita solo las URLs vinculadas, detecta cambios de precio y
   descatalogados, y guarda historial.
4. `core.Comparison` calcula totales por cadena, ganador por producto y compra
   mixta. `report` lo convierte en markdown (`Write`) o en texto para consola
   (`Console`).

## Flujo de ejecución

El comando por defecto (sin subcomando) hace, en este orden: parsear la lista →
resolver solo los productos que falten o tengan coincidencia dudosa → comprobar
precios → escribir el informe → imprimir la comparativa. Los subcomandos
`report` (regenera el informe desde la base), `smoke` (comprueba una ficha de
cada cadena) y `version` son auxiliares. No hay interfaz interactiva.

## Orden de lectura recomendado

1. `cmd/supercomparator/main.go` — cómo se montan las piezas y el flujo.
2. `internal/core/events.go` y `internal/core/core.go` — el contrato del núcleo.
3. `internal/chain/chain.go` — el puerto que cumplen las cadenas.
4. `internal/chain/ahorramas/` — adaptador simple (HTTP + JSON-LD), ideal para empezar.
5. `internal/chain/mercadona/` — adaptador complejo (navegador headless + CP).
6. `internal/list/` y `internal/match/` — lógica pura y sus tests.
7. `internal/store/` — esquema SQLite e historial.
8. `internal/report/` — plantillas de markdown y salida de consola.

## Reglas del proyecto

- Un concepto por fichero; evitar ficheros de más de ~300 líneas.
- Nada de paquetes `utils`, `common` o `helpers`.
- Interfaces definidas en el consumidor (idioma Go).
- `list` y `match` son funciones puras: se testean con golden/fixtures.
- Los textos de salida y docs van en español; los identificadores en inglés.