# Modelo de datos

Todo el catálogo vive en **una sola base SQLite**: `datos/catalogo.db` (82 MB con
las cuatro cadenas indexadas; la copia antigua `datos/precios.db` ya no la usa
nada). El esquema está en `internal/store/migrations.go` y el historial de precios
en `internal/store/migrate.go`. Estas notas explican el diagrama y el diccionario.

## Diagrama entidad-relación

```
        CHAINS                          PRODUCTS
   +--------------+                +------------------+
   |              |                |                  |
   |  1           |------- 1 -----N|                  |
   |              |                |                  |
   +--------------+                +------------------+
        |    |                            |  |   |   |
        |    |                            |  |   |   |
        |    |                        N   |  |   |  1
        |    |        +-------------------+  |   |   |
        |    |        |                       |   |   |
        |    +---1----N|                       |   |   |
        |             | PRICE_HISTORY         |   |   |
        |             | (memoria de precios)  |   |   |
        |             +-----------------------+   |   |
        |                                          |   |
        |         +------------------+              |   |
        +---1-----|  CRAWL_STATE     |              |   |
                  |  (progreso)      |              |   |
                  +------------------+              |   |
                                                     |   |
                  +------------------+              |   |
                  |   PRICE_QUEUE    |<-------------1   |
                  |   (cola)         |  1 producto = 1 trabajo
                  +------------------+                  |
                                                         |
                  +------------------+                  |
                  |    LIST_ITEMS    |<-----------------1
                  |    (mi lista)    |
                  +------------------+

                  +------------------+
                  |   PRODUCTS_FTS   |  1 (indice de busqueda, FTS5)
                  +------------------+
```

## Diccionario de datos

```
CHAINS ------------------------------------------------------------------
  id               PK   text      mercadona | ahorramas | dia | alcampo
  nombre                 text      Mercadona
  sitemap_url            text      de donde se lee el catalogo
  precios_activos        int       0 = Alcampo, espera a que pase el WAF
  pausa_segundos         real      ritmo entre peticiones de esa cadena
  concurrencia           int       cuantas fichas a la vez
  precio_max_horas       int       24: cuando un precio se considera viejo

PRODUCTS -----------------------------------------------------------------
  id               PK   int
  chain            FK   text      -> CHAINS.id
  url              UK   text      ficha en la tienda
  sku                   text
  name                   text      como lo llama la tienda
  search_name            text      normalizado, sin tildes, lematizado
  format                 text      "2 x 500 ml", tal cual
  measure_value          real      1
  measure_unit           text      kg | l
  category               text
  name_source            text      slug | categoria   (cuanto fia el nombre)
  price                  real      1,55
  price_basis            text      unidad | kg       (como viene publicado)
  measure_price          real      por kg o l, para comparar
  available              int
  price_fetched_at       datetime  cuando se comprobo
  crawl_state            text      catalogado | ficha_pendiente
  first_seen             datetime
  last_seen              datetime

PRICE_QUEUE --------------------------------------------------------------
  product_id        PK FK int      -> PRODUCTS.id
  state                 text      pendiente | descargando | error
  attempts              int       intentos ya gastados (MaxIntentos = 5)
  next_attempt_at       datetime  cuándo puede volver a intentarse
  last_error            text      por qué falló la última vez
  updated_at            datetime  cuándo cambió de estado (migración 2)

PRICE_HISTORY ------------------------------------------------------------
  id               PK   int
  product_id       FK   int      -> PRODUCTS.id
  name                 text      nombre en ese momento (puede cambiar)
  price                real
  measure_price        real
  measure_unit         text
  available            int
  fetched_at           datetime

LIST_ITEMS ----------------------------------------------------------------
  id               PK   int
  product_id       FK   int      -> PRODUCTS.id   (un producto, una vez)
  quantity             int
  position             int
  added_at             datetime

CRAWL_STATE --------------------------------------------------------------
  chain            PK FK text      -> CHAINS.id
  phase                 text      donde se ha quedado
  cursor                text      ultima URL procesada
  done                  int
  total                 int
  paused                int
  updated_at            datetime

RUNS ---------------------------------------------------------------------
  id               PK   int
  kind                  text      catalogo | precios
  chain              FK   text      -> CHAINS.id
  started_at            datetime
  finished_at           datetime
  products              int
  errors                int
  note                  text

PRODUCTS_FTS (tabla virtual de FTS5) -------------------------------------
  name
  search_name
```

## Notas sobre el esquema

- **Una fila de `PRODUCTS` por ficha.** La identidad es `UNIQUE(chain, url)`: el
  mismo producto reindexado actualiza su fila y no duplica nada, así que
  `crawl index` se puede repetir cuantas veces haga falta. El reindexado del
  sitemap **no toca** `price`, `measure_price`, `available` ni
  `price_fetched_at`: esos campos son de `store.SetPrice`, que es quien visita la
  ficha. Un catálogo reindexado no puede perder precios.
- **Borrar precio nunca es «poner 0 a mano».** `store.SetPrice` actualiza el
  producto e inserta la muestra en `PRICE_HISTORY` en la misma transacción: no
  hay precios sin rastro.
- **`PRODUCTS_FTS` es una tabla virtual de FTS5 con `rowid = products.id`.** Se
  mantiene sola con los triggers `products_fts_ai` / `_ad` / `_au` de la
  migración 1; no hay ningún código Go que la rellene. Indexa `name` (el nombre
  de la tienda, al que el tokenizador le quita las tildes) y `search_name` (el
  mismo nombre normalizado con `match.Tokens`: sin stopwords y sin plurales, que
  es lo que hace que «fresa» y «fresas» encuentren lo mismo).
- **`PRICE_QUEUE` usa `product_id` como clave primaria**: un producto es un
  trabajo como mucho, aunque falle. `NextQueuedChain` marca las entradas que saca
  como `descargando` dentro de una transacción, para que dos workers no peleen por
  la misma ficha; `FailQueue` sube `attempts` y programa `next_attempt_at` para el
  backoff; `MarkQueueDone` **borra** la fila porque el precio ya está en
  `products` y no hace falta acordarse. `Requeue` rearma a mano una ficha que
  quedó en `error`.
- **`CRAWL_STATE` y `RUNS` son el parte de trabajo.** `crawl_state` es una fila por
  cadena (dónde se quedó, cuánto hizo, si está pausada); `runs` es el historial de
  ejecuciones (`catalogo` o `precios`) que alimenta `GET /api/estado`. `SetPrice`
  no se llama desde el indexador: el indexador solo cataloga y encola.
- **`LIST_ITEMS` está creada pero sin usar.** La tabla de «Mi lista» existe (con
  `UNIQUE(product_id)`, para no añadir dos veces lo mismo) pero todavía no hay
  ningún endpoint ni worker que la toque; es de la fase 2.
- **Marcas de tiempo como texto RFC3339 en UTC** (`store.ts`): se comparan y se
  ordenan en SQL, así que todas tienen que llevar la misma zona.
- **`price_history` conserva columnas del esquema antiguo** (`chain`,
  `product_url`, `brand`, `old_price`…) y `product_id` nullable, porque la base
  puede venir de una versión previa del proyecto. La migración 1 añade
  `product_id` si falta y lo empareja por `(chain, url)`; lo que no encuentra
  ficha se queda sin enlazar en vez de enlazarse al producto equivocado.

## Los estados de `PRICE_QUEUE`

Un producto es un trabajo como mucho, así que la fila es el trabajo y su estado
dice por dónde va:

```
   crawl index
        |
        v
   pendiente ---> descargando ---> (borrada: MarkQueueDone, precio ya en products)
        ^              |
        |              +----> error  (agotados los intentos; espera a que
        |                             alguien la rearme con Requeue)
        |
        +---- FailQueue: sube attempts y pone next_attempt_at con el backoff
```

- **`pendiente`**: trabajo por hacer, esperando su turno. Solo sale de la cola
  cuando `next_attempt_at` ya ha pasado; antes de eso no se toca, y ahí es donde
  viven la pausa de la cadena y el backoff del reintento.
- **`descargando`**: reservado por el worker con `NextQueuedChain`, en la misma
  transacción que lo saca (por eso dos workers nunca pelean por la misma ficha).
  Si el proceso muere aquí, la fila se queda en este estado.
- **`error`**: se acabaron los intentos (`MaxIntentos = 5`), o la tienda
  respondió que el producto ya no existe (`chain.ErrNotFound`, que reintentar no
  arregla). **No se reintenta sola**: espera a que alguien la rearme con
  `Requeue`.
- **`hecho` no es un estado**: una ficha terminada sale de la cola. Lo que dice
  que está hecha es el precio guardado en `products`, y `price_history` es su
  rastro.

## Migración 2: `price_queue.updated_at`

Añade la columna `updated_at` y el índice `idx_queue_estado(state, updated_at)`.
Sin esa marca no hay forma de distinguir una ficha que se está descargando de una
que se quedó a medias al morir el proceso: `last_error` no sirve, porque solo se
escribe cuando algo falla, así que una ficha que pasó a `descargando` sin errores
previos no dejaría rastro. Con `updated_at`, `ReclaimStale(ttl)` devuelve a
`pendiente` lo que lleve en `descargando` más de ese tiempo (un minuto, en
`store`), y lo hace **sin tocar los intentos**: el rescate es por trabajo
perdido, no por un fallo de la tienda.

El esquema solo cambia con una migración nueva al final de
`internal/store/migrations.go`, versionada en `schema_migrations` y aplicada en su
propia transacción; una migración ya aplicada no se toca. Las filas que ya estaban
en la cola se marcan con su `next_attempt_at`, y las que quedaron en
`descargando` arrastran así toda la antigüedad que tenían: es justo lo que las
hace rescatables de inmediato.
