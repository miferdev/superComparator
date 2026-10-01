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
  state                 text      pendiente | descargando | hecho | error
  attempts              int
  next_attempt_at       datetime  respeta la pausa de la cadena
  last_error            text

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
  trabajo como mucho, aunque falle. `NextQueued` marca las entradas que saca como
  `descargando` dentro de una transacción, para que dos workers no peleen por la
  misma ficha; `FailQueue` las devuelve a `pendiente` con `attempts` y
  `next_attempt_at` para el backoff; `MarkQueueDone` **borra** la fila porque el
  precio ya está en `products` y no hace falta acordarse.
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