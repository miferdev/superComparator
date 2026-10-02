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
                                                         |
                  +------------------+                  |
                  | PRECIOS_MANUALES |<-----------------1
                  | (precio a mano)  |  0..1 por producto
                  +------------------+                  |
                                                         |
                  +------------------+                  |
                  |    LIST_ITEMS    |<-----------------1
                  |   (mi compra)    |
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
  quantity             real     cuantas unidades (1,5 kg es legal)
  position             int
  added_at             datetime

PRECIOS_MANUALES (migración 3) -------------------------------------------
  chain            PK FK text      -> CHAINS.id
  product_url      PK FK text      -> PRODUCTS.url  (FK compuesta, ON DELETE CASCADE)
  precio                real      precio de unidad que ha puesto el usuario
  precio_medida         real      por kg o l, si se ha puesto
  medida                text      kg | l | ""  (nada más se guarda)
  nota                  text      de dónde sale ("precio del lineal")
  actualizado           datetime  cuándo lo escribió el usuario

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
- **`LIST_ITEMS` es «Mi compra», y ya tiene API.** Se creó en la migración 1 y
  durante un tiempo no la tocaba nadie; hoy la usan `store.AddToList`,
  `SetListQuantity`, `RemoveFromList`, `ClearList` y `ListaCompra`, que son los que
  están detrás de `/api/mi-compra`. `UNIQUE(product_id)` está para no añadir dos
  veces lo mismo: un producto que ya está en la lista cambia de cantidad, y eso se
  dice con un error, no sumando por sorpresa. `quantity` se maneja como número
  real aunque la columna se crease como `INTEGER`: `1,5` kg de plátanos es una
  cantidad legítima.
- **La línea de `LIST_ITEMS` no guarda precio: lo resuelve en el momento de
  leerla.** `store.ListaCompra` junta `list_items` con `products` y con
  `precios_manuales` y trae ya el precio efectivo con el `CASE WHEN`: si hay
  precio manual, ese manda sobre el de la tienda, tanto en `precio` como en
  `precio_medida`. El precio que se ve, en `list_items` no está, así que
  borrarlo no puede desincronizar nada y cambiarlo se nota en la siguiente
  lectura.
- **`PRECIOS_MANUALES` es un dato de otra fuente y por eso va en su propia
  tabla.** Alcampo está detrás de un WAF que responde 403, así que el usuario
  escribe a mano el precio que ve en el tienda. Ni `products.price` ni
  `price_history` se tocan al guardarlo: un dato puesto por una persona no puede
  pasar por dato de la web, ni al revés. Por eso tampoco hay
  `precio_history` de precios manuales: su rastro es `actualizado`.
- **`PRECIOS_MANUALES` está atada a `PRODUCTS` por `(chain, product_url)`**, que
  es la `UNIQUE(chain, url)` del catálogo, con `ON DELETE CASCADE`. Consecuencia
  práctica: un precio manual solo puede existir para un producto real del
  catálogo (`store.SetPrecioManual` lo comprueba antes de escribir) y, si el
  producto se borra, el precio manual se va con él en vez de quedarse hablando de
  una ficha que ya no existe.
- **La clave de `PRECIOS_MANUALES` es `(chain, product_url)`, no `product_id`**
  para no tener una segunda manera de identificar un producto en el esquema: es
  el mismo par que usa `products` y que llega en la URL de la API.
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

## Migración 3: `precios_manuales`

Crea una sola tabla, `precios_manuales`, y **no toca `list_items`**: esa tabla ya
existía desde la migración 1 y lo único que le faltaba eran los endpoints, que son
código y no esquema.

La tabla existe por Alcampo. Su catálogo se lee muy bien (89.615 productos) pero
las fichas están detrás de un WAF que responde 403 a los clientes automatizados, así
que no sale ningún precio de él y **no se intenta saltarse ese WAF**. Para poder
saber cuánto costaría la compra en esa tienda, el usuario escribe el precio que ve
en el lineal. La tabla recoge ese dato, con dos reglas que no son de estilo sino de
sentido:

- **No se mezcla con el precio de la tienda.** Ni `products.price` ni
  `price_history` se tocan al guardar un precio manual, ni al revés. Son fuentes
  distintas y el esquema las mantiene separadas para que no se confundan: un `0`
  en `products.price` significa «la tienda no publica precio», no «el precio está
  en otra tabla».
- **Solo para productos que existen.** La FK compuesta `(chain, product_url) →
  products(chain, url)` hace las dos cosas de una vez: impide guardar un precio
  para una ficha que no está en el catálogo (que es un error con mensaje en
  español, no una fila huérfana) y hace que al borrar el producto se borre su
  precio manual.

Como el resto del esquema, esto se aplica con una migración versionada en
`schema_migrations` y en su propia transacción; `migración 3 (precios puestos a
mano)`.
