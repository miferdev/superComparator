# Arquitectura

SuperComparator es un programa de línea de comandos que compara precios de la
compra entre Mercadona, Ahorramas y DÍA, y escribe un informe markdown por
supermercado más otro final. Esta guía explica las piezas y el orden recomendado
de lectura.

## Vista general

```
cmd/supercomparator/main.go     punto de composición y flujo por defecto (~80 líneas)
internal/
  config/    env + flags          no depende de nadie
  list/      parser de lista.md   puro, sin red ni BD
  match/     normalización/ranking puro, sin red ni BD
  chain/     interfaz (puerto) Chain + tipos Product/SitemapEntry/Alternative
    browser/    Chromium headless compartido por las cadenas que lo necesitan
    mercadona/  rod headless (CP 28032, /sitemap.xml y /product)
    ahorramas/  HTTP + JSON-LD (sitemaps y fichas)
    dia/        HTTP + JSON-LD, con Lookup por categoría (su sitemap no lleva nombre)
    alcampo/    sitemaps por HTTP + rod (WAF; desactivada por defecto)
  store/     SQLite (items, matches, price_history, alternatives)
  core/      orquestador + eventos; usa chain, match y store
  report/    informes por cadena, comparativa y consola (depende de los tipos de core)
```

Regla de dependencias: `cmd → core → chain|match|store → config`, con `report`
colgando de `core`. Las flechas van en un solo sentido; no hay ciclos. Los
adaptadores de cadena solo importan `chain`, `config` y `match` (que es puro: sin
red ni base de datos).

## Flujo de datos

1. `list` parsea la tabla markdown y devuelve `[]list.Item`.
2. `core.Resolve` pide candidatos a cada `chain.Chain`. Por defecto los saca del
   sitemap y los puntúa con `match.Rank`; si la cadena implementa `chain.Lookup`
   (DÍA), es ella quien busca y ordena, y el núcleo se fía. Descarga los mejores
   y los puntúa con `match.Similarity` sobre el nombre real de la ficha. Entre los
   que superan `match.AutoThreshold` gana el más barato; por debajo del umbral el
   producto **no** entra en la comparativa: se guarda como alternativa y el informe
   lo manda a «Revisar».
3. `core.Check` revisita solo las URLs vinculadas, detecta cambios de precio y
   descatalogados, y guarda historial.
4. `core.Comparison` calcula totales por cadena, ganador por producto y compra
   mixta, y añade los productos sin resolver, los cambios de precio recientes y
   lo que hay que revisar. `report.WriteAll` escribe un markdown por cadena
   (`report.go`, `chains.go`) más la comparativa (`report.go`), y `Console` lo
   muestra en texto plano.

## Flujo de ejecución

El comando por defecto (sin subcomando) hace, en este orden: parsear la lista →
resolver solo lo que falte (un producto nuevo, una cadena que no lo tenía o una
coincidencia dudosa) → comprobar precios → escribir los informes → imprimir la
comparativa. `--cadenas` elige qué cadenas se comparan. Los subcomandos `report`
(regenera los informes desde la base), `smoke` (comprueba una ficha de cada
cadena) y `version` son auxiliares. No hay interfaz interactiva.

## Orden de lectura recomendado

1. `cmd/supercomparator/main.go` — cómo se montan las piezas y el flujo.
2. `internal/core/events.go` y `internal/core/core.go` — el contrato del núcleo.
3. `internal/chain/chain.go` — el puerto que cumplen las cadenas.
4. `internal/chain/ahorramas/` — adaptador simple (HTTP + JSON-LD), ideal para empezar.
5. `internal/chain/dia/` — adaptador con `Lookup`: el catálogo no se puede
   resolver con el sitemap y la cadena busca ella misma.
6. `internal/chain/mercadona/` — adaptador con navegador headless y CP.
7. `internal/list/` y `internal/match/` — lógica pura y sus tests.
8. `internal/store/` — esquema SQLite e historial.
9. `internal/report/` — informes por cadena, comparativa y salida de consola.

## Reglas del proyecto

- Un concepto por fichero; evitar ficheros de más de ~300 líneas.
- Nada de paquetes `utils`, `common` o `helpers`.
- Interfaces definidas en el consumidor (idioma Go).
- `list` y `match` son funciones puras: se testean con golden/fixtures.
- Los textos de salida y docs van en español; los identificadores en inglés.