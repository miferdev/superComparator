# Arquitectura

SuperComparator indexa los catálogos de Mercadona, Ahorramas, DÍA y Alcampo en
una base SQLite y sirve una API JSON para buscar productos y comparar precios.
Esta guía explica las piezas, por qué están donde están y qué está hecho y qué
no.

## Vista general

```
cmd/supercomparator/   CLI: serve (por defecto), crawl index, smoke, version
internal/
  catalog/              indexer.go (indexa sitemaps) + catalog.go (consultas y DTO)
  chain/                puerto Chain + browser/ + mercadona/ + ahorramas/ + dia/ + alcampo/
  match/                normalización y lematización (puro, sin red)
  store/                SQLite: migrations.go, catalog.go, search.go, queue.go, pricing.go, crawl.go, runs.go, prices.go
  server/               HTTP + JSON
  version/              versión y commit
  config/               env
```

Y dos documentos de referencia: [docs/modelo-datos.md](docs/modelo-datos.md)
(esquema de `datos/catalogo.db`) y [docs/diagrama-flujo.md](docs/diagrama-flujo.md)
(qué pasa al arrancar, al indexar y al usar la web).

## Regla de dependencias

```
cmd → server → catalog → chain | match | store

chain                  puerto; no importa nada interno
match → chain          solo por el alias Measure = chain.Measure
store → chain          residuo de prices.go, solo tipos (ver abajo)
chain/mercadona|ahorramas|dia|alcampo → chain, browser, config   (dia → + match)
browser → config
config                 no depende de nadie
```

Las flechas van en un solo sentido y no hay ciclos. Los detalles que importan:

- **`server` solo depende de `catalog`, `store` y `version`.** Es la regla que
  sostiene la separación: la API **no** conoce ningún adaptador de tienda. Un
  endpoint no puede bajarse a la red; si necesita un dato, o no está en la base,
  o se escribe un comando nuevo que sí use `chain`.
- **`catalog` es el orquestador**: usa `chain` para leer catálogos, `match` para
  normalizar nombres y `store` para guardar. Es el único paquete que conoce las
  tres cosas.
- **`chain` es el puerto y no importa nada interno.** Lo consumen `catalog`, los
  adaptadores (`mercadona`, `ahorramas`, `dia`, `alcampo`) y `store`, y define
  `Chain` (`ID`, `Sitemap`, `Fetch`), `SitemapEntry`, `Product` y la extensión
  opcional `Lookup`. Los adaptadores **no se conocen entre sí**: para añadir una
  tienda se escribe un paquete nuevo y se engancha en
  `cmd/supercomparator.selectChains`.
- **`match` es puro** (sin red, sin base). Lo usan `catalog` (para `search_name`)
  y el adaptador de DÍA. `match.Measure` es un alias de `chain.Measure` para no
  duplicar el tipo; por eso aparece `match → chain` en el import graph.
- **`store → chain` es residuo**: `store/prices.go` conserva tipos del esquema
  antiguo de la lista de la compra (y `LastMatchPrices` consulta una tabla
  `matches` que ya no existe). Es código muerto a la espera de que la fase 2
  decida si se borra o se reutiliza. **No escribas código nuevo ahí.**
- **`config` no depende de nadie** y lo leen `cmd` y los adaptadores que
  necesitan el Chromium (`browser`) o el código postal.

## Flujo de datos

1. **Indexado** (`crawl index`): `cmd` monta los adaptadores con
   `selectChains`, abre la base con `store.Open` (migraciones + semilla de
   cadenas) y llama a `catalog.Indexer.IndexCatalog`, que va cadena por cadena.
   Una cadena que falla no tira el resto: el error se anota en su `crawl_state` y
   se sigue. De cada `SitemapEntry` sale un producto (`productFromEntry`): el
   nombre va tal cual y a `search_name` le pasa `match.Tokens`, que quita
   tildes, stopwords y plurales; la medida sale de `chain.FormatFromName` +
   `match.ParseMeasure`. `store.UpsertProducts` los guarda a lotes de 500 en una
   transacción, con `ON CONFLICT(chain, url) DO UPDATE` **sin tocar el precio**
   (el precio es de `store.SetPrice`, no del reindexado).
2. **Encolado**: al terminar la cadena, `store.EnqueuePrices` mete en
   `price_queue` los productos sin precio que no estaban ya encolados, y el
   indexador anota el parte en `crawl_state` y en `runs`.
3. **Precios** (fase 2): el worker de la cola consume `store.NextQueued`, espera
   el ritmo de la cadena, llama a `chain.Chain.Fetch`, y guarda con
   `store.SetPrice`, que actualiza el producto e inserta la muestra en
   `price_history` en la misma transacción.
4. **Lectura**: `GET /api/catalogo` → `catalog.NormalizeSearchQuery` →
   `store.Search` (FTS5 o `LIKE`) → `catalog.toProduct`, que añade lo que la web
   necesita y no está en la tabla: el nombre comercial de la cadena
   (`nombreCadena`), `tienePrecio`, `precioViejo` y `frescuraHoras` contra el
   `precio_max_horas` de esa cadena.

## Por qué la cola de precios vive en SQLite

Porque tiene que **sobrevivir al proceso**. Descargar 106.000 fichas con pausas
por cadena tarda horas o días; si la cola estuviera en memoria, cerrar el
contenedor perdería el trabajo hecho y las entradas ya fallidas, y no volvería
a empezar. En tablas:

- Se **reanuda sola** al arrancar: `price_queue` dice qué queda y con qué
  `next_attempt_at`. No hay cron ni estado que reconstruir.
- El **ritmo por cadena es dato, no código**: `chains.pausa_segundos`,
  `chains.concurrencia` y `price_queue.next_attempt_at` son ajustables por SQL.
- **`NextQueued` es una reserva con transacción**: marca lo que saca como
  `descargando` dentro de la misma transacción, así que dos workers nunca pelean
  por la misma ficha; si el proceso muere a medias esas filas se quedan
  `descargando` (se pueden volver a poner en `pendiente` con un `UPDATE`).
- El **backoff es un número guardado**: `attempts` y `next_attempt_at`; el fallo
  (con su `last_error`) queda registrado, que es lo que permite ver por qué una
  cadena no avanza.
- `crawl_state` juega el mismo papel para el indexado: dónde se quedó cada
  cadena.

La contrapartida es que `store` necesita escribir y leer la cola desde varios
hilos; con `SetMaxOpenConns(1)` y WAL, un único escritor y lectores concurrentes
lo hacen seguro sin bloqueos.

## Por qué SQLite y no Postgres

El problema real es **un usuario, un escritor y 106.000 filas**: no hay un
servidor de base de datos que gestionar, ni usuarios, ni red, ni backups
coordinados. SQLite encaja porque:

- **Un solo fichero** (`datos/catalogo.db`, 82 MB) que se copia, se borra y se
  regenera con `crawl index`. Sin servicio que levantar.
- **Driver puro Go** (`modernc.org/sqlite`): `CGO_ENABLED=0` en el Dockerfile, así
  que la imagen es un binario estático y no hace falta toolchain de C.
- **WAL + un escritor**: el indexado escribe a lotes de 500 mientras la web lee
  sin bloquearse; `SetMaxOpenConns(1)` elimina los `database is locked` sin
  tener que negociar `busy_timeout` a mano (además se fija a 5 s).
- **FTS5 viene dentro**: la tabla virtual de búsqueda es una característica del
  propio motor, sin Lucene ni extensiones que instalar ni un segundo servicio que
  mantener sincronizado.

Postgres se pagaría solo si algún día hiciera falta: varios usuarios escribiendo a
la vez, varias réplicas del scraper, o consultas geoespaciales. Hoy nada de eso
existe, y meterlo añadiría un contenedor que levantar y un esquema de permisos
que nadie usa.

## FTS5 y cómo se mantiene

`products_fts` es una tabla virtual `fts5(name, search_name)` con
`tokenize = "unicode61 remove_diacritics 2"`, y su `rowid` **es** `products.id`.
No hay código Go que la rellene: la migración 1 crea tres triggers
(`products_fts_ai`, `_ad`, `_au`) que insertan, borran y reinsertan la fila
sensible a cada cambio de `products`. Consecuencias, que son las reglas a
respetar:

- **Ni una línea de Go escribe en `products_fts`.** Si algún día hace falta
  indexar otro campo (marca, categoría), se añade a la tabla virtual y a los
  triggers, en una migración nueva; no se hace un `INSERT` suelto porque el
  `UPDATE` de un producto lo borraría.
- **Las dos columnas se buscan.** `name` va tal cual y el tokenizador
  (`remove_diacritics`) le quita tildes, así que escribir «fresas» sin tilde
  encuentra «fresas bandeja» (65 filas). `search_name` añade la forma
  normalizada de `match.Tokens` (sin stopwords, sin plurales), de modo que
  «fresa» encuentra lo mismo y también lo que solo está en esa forma.
- **El `MATCH` se construye con comillas y comodín de prefijo**
  (`store.ftsQuery`: `"fresas"*`), así que un término raro no rompe la sintaxis y
  «fres» encuentra «fresas». Antes de tirar a FTS5, `store.Search` **cae a un
  `LIKE` sobre `search_name`** en lugar de devolver un error.
- El orden por defecto de una búsqueda con texto es `bm25(products_fts)`, que es
  lo que da la relevancia; sin texto se ordena por nombre.

## Estado real de cada fase

| Pieza | Estado | Notas |
|-------|--------|-------|
| Esquema SQLite + migraciones + FTS5 con triggers | **hecho** | Migración 1; `schema_migrations` versiona lo aplicado |
| Lectura de catálogos por sitemap en las 4 cadenas | **hecho** | 106.226 productos en `datos/catalogo.db` |
| Indexado idempotente (`crawl index`) | **hecho** | Repetible, no duplica ni pisa precios |
| API JSON (estado, cadenas, búsqueda, producto) | **hecho** | Búsqueda en 4-26 ms sobre 106.226 filas |
| Marcado de precio viejo (`precioViejo`, `frescuraHoras`) | **hecho** | Solo informa: aún no hay recomprobación |
| Estado de la cola en SQLite (`price_queue`) y su API de `store` | **hecho** | `EnqueuePrices`, `NextQueued`, `FailQueue`, `MarkQueueDone` |
| **Worker de la cola de precios** | **pendiente** | Es lo que pondría los precios: falta el bucle que consume la cola |
| Reanudar el indexado desde `crawl_state` | **pendiente** | Hoy `crawl index` relee el sitemap entero (el upsert hace que sea seguro) |
| Recomprobación de un precio viejo al abrirlo + SSE | **pendiente** | |
| SPA de Angular | **pendiente** | Hoy `GET /` es una página mínima que lista la API |
| «El mismo producto en otras tiendas» | **pendiente** | No hay agrupación entre cadenas |
| «Mi lista» (`list_items`) con subtotales | **pendiente** | La tabla existe, vacía y sin endpoints |
| Limpiar `store/prices.go` (herencia de la lista de la compra) | **pendiente** | `LastMatchPrices` consulta una tabla que ya no existe |

## Orden de lectura recomendado

1. `cmd/supercomparator/main.go` — Flags, semilla de las cuatro cadenas y
   `runServe`.
2. `cmd/supercomparator/crawl.go` — El comando `crawl index` y `smoke`.
3. `internal/server/server.go` — Las rutas de la API (y que no toca ninguna
   tienda).
4. `internal/catalog/catalog.go` — Qué ve la web: DTO, cadenas y frescura.
5. `internal/catalog/indexer.go` — Del sitemap a `products`.
6. `internal/chain/chain.go` — El puerto `Chain` y sus tipos.
7. `internal/chain/ahorramas/` — Adaptador sencillo (HTTP + JSON-LD), el mejor
   para empezar.
8. `internal/chain/mercadona/` o `internal/chain/alcampo/` — Adaptadores con
   navegador headless (`internal/chain/browser`).
9. `internal/store/migrations.go` — El esquema y los triggers de FTS5.
10. `internal/store/search.go` — Cómo se busca y cómo se ordenan los resultados.
11. `internal/store/queue.go` y `pricing.go` — La cola y la escritura de precios.
12. `internal/match/match.go` — Normalización y lematización.

## Reglas del proyecto

- Un concepto por fichero; evitar ficheros de más de ~300 líneas. Nada de
  paquetes `utils`, `common` o `helpers`.
- Interfaces definidas en el consumidor (idioma Go).
- `match` es puro: se testea con fixtures, sin red ni base de datos.
- La web no habla con las tiendas: si un endpoint necesita un dato que no está
  en la base, se escribe un comando (`crawl`), no un handler.
- Identificadores en inglés; salida, docs y commits en español.
- `gofmt`, `go vet ./...` y `go test ./...` deben pasar antes de commitear (lo
  mismo que CI).