# Arquitectura

SuperComparator indexa los catálogos de Mercadona, Ahorramas, DÍA y Alcampo en
una base SQLite y sirve una API JSON para buscar productos y comparar precios.
Esta guía explica las piezas, por qué están donde están y qué está hecho y qué
no.

## Vista general

```
cmd/supercomparator/   CLI: serve (por defecto), crawl index, crawl precios, smoke, version
internal/
  catalog/              indexer.go (indexa sitemaps) + catalog.go (consultas y DTO) + fase3.go (mi compra y precios a mano) + prices.go (trabajo de precios)
  chain/                puerto Chain + browser/ + mercadona/ + ahorramas/ + dia/ + alcampo/
  match/                normalización y lematización (puro, sin red)
  store/                SQLite: migrations.go, catalog.go, search.go, queue.go, priceloop.go, priceritmo.go, pricing.go, crawl.go, runs.go, lista.go, manual.go, prices.go
  server/               HTTP + JSON: server.go (rutas), micompra.go, preciosmanuales.go, dominio.go (errores)
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
  o se escribe un comando nuevo que sí use `chain`. «Mi compra» y los precios a
  mano (`micompra.go`, `preciosmanuales.go`) son el ejemplo de lo que sale de esa
  regla: no hablan con ninguna tienda, solo leen y escriben en la base a través de
  `catalog`, que es quien traduce a DTO.
- **`catalog` es el orquestador**: usa `chain` para leer catálogos, `match` para
  normalizar nombres y `store` para guardar. Es el único paquete que conoce las
  tres cosas.
- **El worker de la cola vive en `store` y no conoce ninguna tienda.** Consume
  `price_queue` con `store.PriceFetcher` (`Fetch(ctx, chainID, url) →
  chain.Product`), una interfaz de una sola línea que implementa `catalog`
  (`NewFetcher`) con los adaptadores que le pasa quien lo arma
  (`catalog.NewPricesJob`). Por eso `store` puede tener el bucle sin que ningún
  handler suyo baje a la red.
- **`catalog.PricesJob` es el pegamento y por qué existe:** el bucle sabe cuánto
  esperar y qué ficha tocar, pero no sabe tokenizar un nombre (eso es `match`, y
  `match` solo puede vivir en `catalog`). De ahí que el enganche sea la función
  `PriceLoop.WithOnFicha`: el bucle le pasa el producto recién descargado y
  `catalog` guarda el nombre y la categoría definitivos. Mover esa normalización
  a `store` haría que `store` importara `match` y rompería el grafo.
- **`chain` es el puerto y no importa nada interno.** Lo consumen `catalog`, los
  adaptadores (`mercadona`, `ahorramas`, `dia`, `alcampo`) y `store`, y define
  `Chain` (`ID`, `Sitemap`, `Fetch`), `SitemapEntry`, `Product` y la extensión
  opcional `Lookup`. Los adaptadores **no se conocen entre sí**: para añadir una
  tienda se escribe un paquete nuevo y se engancha en
  `cmd/supercomparator.selectChains`.
- **`config` no depende de nadie** y lo leen `cmd` y los adaptadores que
  necesitan el Chromium (`browser`) o el código postal. `selectChains` recibe la
  `config.Config` **entera** y a propósito: montando una config de mentira con
  solo el navegador, Mercadona devolvía «producto no encontrado» en todas sus
  fichas, porque ni fijaba la tienda (código postal) ni esperaba al render
  (timeout), y el error no lo explicaba.
- **`match` es puro** (sin red, sin base). Lo usan `catalog` (para `search_name`)
  y el adaptador de DÍA. `match.Measure` es un alias de `chain.Measure` para no
  duplicar el tipo; por eso aparece `match → chain` en el import graph.
- **`store → chain` es residuo**: `store/prices.go` conserva tipos del esquema
  antiguo de la lista de la compra (y `LastMatchPrices` consulta una tabla
  `matches` que ya no existe). Es código muerto a la espera de que la fase 2
  decida si se borra o se reutiliza. **No escribas código nuevo ahí.**

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
3. **Precios**: el worker de la cola (`store.PriceLoop`) reparte el trabajo por
   turnos entre las cadenas con `store.NextQueuedChain`, que **reserva** en la
   misma transacción marcando `descargando`; cada flujo de cada cadena duerme su
   `pausa_segundos` (`priceritmo.go`) y llama a `PriceFetcher.Fetch`, que es el
   adaptador de esa tienda. Si la ficha trae precio, `OnFicha` deja que `catalog`
   escriba el nombre y la categoría (`store.FichaData`) y luego `store.SetPrice`
   actualiza el producto e inserta la muestra en `price_history` en la misma
   transacción; `MarkQueueDone` borra la fila de la cola. Si falla,
   `store.FailQueue` sube `attempts` y programa el siguiente intento con el
   backoff, y la pasada sigue con la siguiente ficha.
4. **Lectura**: `GET /api/catalogo` → `catalog.NormalizeSearchQuery` →
   `store.Search` (FTS5 o `LIKE`) → `catalog.toProduct`, que añade lo que la web
   necesita y no está en la tabla: el nombre comercial de la cadena
   (`nombreCadena`), `tienePrecio`, `precioSoloMedida`, `precioViejo` y
   `frescuraHoras` contra el `precio_max_horas` de esa cadena, y `precioFuente`,
   que sale de mirar si esa ficha tiene un precio puesto a mano.
5. **Precios a mano** (`PUT /api/precios-manuales`): `server` valida el cuerpo y
   `catalog.SetPrecioManual` lo pasa a `store.SetPrecioManual`, que **valida antes
   de escribir** y guarda en `precios_manuales`. Ni `products` ni `price_history`
   se tocan. A partir de ahí el precio manual se ve en cualquier lectura: la
   búsqueda lo resuelve para todos los resultados de golpe (una consulta, no una
   por producto) y `GET /api/producto` lo pone por delante del de la tienda.
6. **Mi compra** (`/api/mi-compra`): `catalog` llama a `store` (`AddToList`,
   `SetListQuantity`, `RemoveFromList`, `ClearList`, `ListaCompra`) y traduce la
   lista a DTO. El precio efectivo de cada línea se resuelve **en SQL**, en la
   misma consulta de `ListaCompra`, con un `CASE WHEN` sobre `precios_manuales`:
   si hay precio manual, ese es el que suma. Los totales se redondean a céntimos en
   `catalog`, no en `store`, porque son sumas de flotantes y sin redondear un total
   que se enseña como dinero saldría como `5.6000000000000005`.

El trabajo de precios no bloquea nada: `serve` lo lanza en segundo plano con
`catalog.NewPricesJob(...).Run(ctx)`, así que la web levanta y responde mientras
la cola tarda horas. Y `GET /api/eventos` (Server-Sent Events) publica cada 2 s
el estado de la cola por cadena —`pendiente`, `descargando`, `error` y
`precios`— leyéndolo de la base, no del bucle.

## Por qué la cola de precios vive en SQLite

Porque tiene que **sobrevivir al proceso**. Descargar 106.000 fichas con pausas
por cadena tarda horas o días; si la cola estuviera en memoria, cerrar el
contenedor perdería el trabajo hecho y las entradas ya fallidas, y no volvería
a empezar. En tablas:

- Se **reanuda sola** al arrancar: `price_queue` dice qué queda y con qué
  `next_attempt_at`. No hay cron ni estado que reconstruir.
- El **ritmo por cadena es dato, no código**: `chains.pausa_segundos`,
  `chains.concurrencia` y `price_queue.next_attempt_at` son ajustables por SQL.
- **`NextQueuedChain` es una reserva con transacción**: marca lo que saca como
  `descargando` dentro de la misma transacción, así que dos workers nunca pelean
  por la misma ficha. Si el proceso muere a medias esas filas se quedan
  `descargando`, y `ReclaimStale` las devuelve a `pendiente` por antigüedad: para
  eso está `price_queue.updated_at` (migración 2), que es lo que distingue una
  descarga en curso de una que se quedó atascada.
- El **backoff es un número guardado**: `attempts` y `next_attempt_at`; el fallo
  (con su `last_error`) queda registrado, que es lo que permite ver por qué una
  cadena no avanza. A los `MaxIntentos` (5) la ficha queda en `error` y espera a
  que alguien la rearme con `Requeue`.
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

## El precio, tal cual lo publica la tienda

Cada tienda publica el precio de una manera y el modelo no la corrige, porque
corregirla es inventar. Dos casos que se confunden y que están en el tipo:

- **Vendido al peso** (plátanos a 1,65 €/kg): no hay precio de unidad, solo el de
  la medida. `chain.Product` lo dice con `PriceIsPerMeasure` y su método
  `PrecioEsPorMedida()`, y `Price`/`UnitPrice` valen 0 **a propósito**. Quien
  llama no tiene que deducirlo comparando números, y el DTO lo expone como
  `precioSoloMedida` para que el 0 no se lea como un dato que falta.
- **Precio de unidad con el de la medida aparte** (una botella de vino a 3,65 €
  que además está a 4,87 €/l): `price_basis` es `unidad`, porque
  `products.price` es el precio que se enseña y el que se sumaría al total. La
  base solo pasa a ser la medida cuando no hay precio de unidad.

Lo mismo con el texto: el precio del JSON-LD se lee con `chain.ParseJSONNumber`
(formato máquina) y nunca con `chain.ParsePrice` (formato español); confundirlas
no da error, da `1.65` guardado como `1` y los productos de menos de un euro sin
precio. Y `chain.UnescapeText` solo se aplica al texto crudo del JSON-LD, nunca
al del DOM, que `net/html` ya viene desescapado. Las reglas completas están en
[AGENTS.md](AGENTS.md#desescapar-y-parsear-cada-formato-con-su-función).

Y el precio que pone el usuario **no corrige** el de la tienda: es otro dato, de
otra fuente, que vive en su propia tabla (ver «El precio que pone el usuario, al
lado del de la tienda»).

## El precio que pone el usuario, al lado del de la tienda

Son dos fuentes y no se mezclan. Alcampo está detrás de un WAF que responde 403 a
los clientes automatizados, así que sus 89.615 fichas están catalogadas pero sin
precio y **no se intenta saltarse ese WAF**: la solución es que el usuario escriba
a mano el precio que ve en la tienda. Lo mismo sirve para un precio que haya visto
en el lineal y no esté en la web.

De ahí salen las cuatro decisiones que explican el resto del diseño:

- **Tabla propia (`precios_manuales`, migración 3).** Ni `products.price` ni
  `price_history` se tocan al guardar un precio a mano, ni al revés: un dato puesto
  por una persona no puede pasar por dato de la web. También significa que un `0`
  en `products.price` sigue significando «la tienda no publica precio», y no «el
  precio está en otro sitio». El precio manual no tiene `price_history`: su rastro
  es su propia marca de tiempo.
- **Identidad `(chain, url)` con FK a `products`.** Un precio manual solo puede
  existir para un producto real del catálogo —`store.SetPrecioManual` lo comprueba
  y, si no está, lo dice en vez de dejar una fila huérfana— y, si el producto se
  borra, se borra con él (`ON DELETE CASCADE`). Es la misma identidad que usa el
  catálogo entero, no una segunda forma de nombrar un producto.
- **Valida antes de escribir.** Tiene que haber precio de unidad o precio de
  medida, y la medida solo puede ser `kg` o `l` (un «€ por bolsa» no se puede
  comparar con un €/kg). Si algo no cuadra, se devuelve el error y no se escribe
  nada: es preferible que el usuario se entere a que se guarde un precio a medias
  creyendo que sí.
- **Cuenta como recién escrito.** `aplicaPrecioManual` (`catalog`) pone
  `precioComprobado` a la marca del precio manual y `frescuraHoras` a 0, así que
  el producto no sale como `precioViejo` aunque el precio de la tienda sea viejo:
  el dato se acaba de escribir, está fresco por definición.

`precioFuente` dice de dónde sale el precio **de unidad** que se enseña y se suma:
`web`, `manual` o `ninguno`. El matiz de `ninguno` importa: **no** significa «no hay
ningún precio», sino «no hay precio de unidad». Un producto vendido al peso sale
con `ninguno` aunque su €/kg venga de la web, porque no hay precio de unidad que
enseñar ni que sumar.

## «Mi compra»: qué suma el total y qué no

`list_items` solo guarda qué productos y con qué cantidad. **El precio de una línea
no está en la lista**: se resuelve al leer, en `store.ListaCompra`, juntando
`list_items` con `products` y con `precios_manuales`. Consecuencias que no son de
estilo:

- El precio manual **manda** sobre el de la tienda para el subtotal de su línea, y
  la línea lo dice (`precioFuente: "manual"`), porque un total que mezcla las dos
  fuentes sin avisar no se puede leer.
- **El total solo suma precios de unidad.** Lo vendido al peso (plátanos a 1,65
  €/kg) no tiene precio de unidad: su `price` está a 0 **a propósito** y de un €/kg
  no se puede saber cuánto cuesta una bolsa. Esas líneas no se estiman, se cuentan
  aparte: multiplicar el €/kg por una cantidad inventada daría un total falso, y
  **un total falso es peor que un total con huecos**. `SinPrecio` las cuenta,
  `sinPrecioDetalle` las nombra y `avisoTotal` lo explica en una frase (vacío cuando
  no hay ninguna).
- `SinPrecio` cuenta **toda línea con precio <= 0**, así que no es solo «lo vendido
  al peso»: también entra lo que todavía no tiene precio (una ficha de Alcampo sin
  precio escrito, o una que la cola aún no ha descargado). Documentarlo como «solo
  lo vendido al peso» sería falso.
- `subtotalPorCadena` solo trae las tiendas con algo sumable, ordenado de mayor a
  menor, que es como lo pinta una web; una tienda con líneas al peso y líneas con
  precio aparece con la parte sumable y sin el resto.
- **Cantidad 0 o menor quita el producto**, y lo decide `catalog.CambiarCantidad`
  (que llama a `RemoveFromList`), no el handler. Dejar una línea a cero haría que el
  total pareciera completo sin serlo.

Que la cantidad llegue como `*float64` en `POST /api/mi-compra` es a propósito:
ausente significa 1 (lo que quiere decir el botón de «añadir») y un 0 explícito
significa quitar. En `PUT`, cantidad ausente es error, porque un `PUT` sin cantidad
no puede querer decir «quítalo» y quitándolo en silencio.

## Los errores de la lista y de los precios a mano no son fallos del servidor

`internal/server/dominio.go` (`writeErrDominio`) decide el status mirando el mensaje
que devuelve `store`, que ya viene en español:

- **404** para «no existe»: los mensajes que contienen `no está en el catálogo` o
  `no está en la lista`.
- **400** para el resto de errores de dominio: cantidad <= 0, producto ya en la
  lista, medidas incoherentes, JSON inválido, falta un campo obligatorio.
- **500** para lo que no sea un error de petición: lo que no aparece en ninguna de
  esas listas se deja en 500 a propósito, para no disfrazar un fallo de SQLite de
  petición mala.

## Estado real de cada fase

| Pieza | Estado | Notas |
|-------|--------|-------|
| Esquema SQLite + migraciones + FTS5 con triggers | **hecho** | Migración 1; `schema_migrations` versiona lo aplicado |
| Lectura de catálogos por sitemap en las 4 cadenas | **hecho** | 106.226 productos en `datos/catalogo.db` |
| Indexado idempotente (`crawl index`) | **hecho** | Repetible, no duplica ni pisa precios |
| API JSON (estado, cadenas, búsqueda, producto) | **hecho** | Búsqueda en 4-26 ms sobre 106.226 filas |
| Marcado de precio viejo (`precioViejo`, `frescuraHoras`) | **hecho** | Solo informa: aún no hay recomprobación |
| Estado de la cola en SQLite (`price_queue`) y su API de `store` | **hecho** | `EnqueuePrices`, `NextQueued`, `FailQueue`, `MarkQueueDone` |
| **Worker de la cola de precios** | **hecho** | `store.PriceLoop` (`priceloop.go`) consume la cola; `serve` lo arranca en segundo plano, `crawl precios` hace una pasada |
| Nombre y categoría definitivos desde la ficha (`store.FichaData`) | **hecho** | Lo que arregla los productos de DÍA, que solo tenían la categoría |
| SSE del progreso de la cola (`GET /api/eventos`) | **hecho** | Un evento `estado` cada 2 s por cadena |
| **Precios puestos a mano** (`precios_manuales`, `/api/precios-manuales`) | **hecho** | Migración 3; tabla aparte de `products.price`, con `precioFuente` en el DTO de producto. Es lo que da precio a Alcampo, detrás de su WAF |
| **«Mi compra»** (`list_items`, `/api/mi-compra`) | **hecho** | La tabla venía de la migración 1; ahora tiene API. El total solo suma precios de unidad y las líneas sin precio de unidad se cuentan aparte (`sinPrecio`, `avisoTotal`) |
| Reanudar el indexado desde `crawl_state` | **pendiente** | Hoy `crawl index` relee el sitemap entero (el upsert hace que sea seguro) |
| Recomprobación de un precio viejo al abrirlo | **pendiente** | El SSE ya avisa del progreso, no de un precio caducado |
| SPA de Angular | **pendiente** | Hoy `GET /` es una página mínima que lista la API; «Mi compra» y los precios a mano son endpoints, no interfaz |
| «El mismo producto en otras tiendas» | **pendiente** | No hay agrupación entre cadenas |
| Limpiar `store/prices.go` (herencia de la lista de la compra) | **pendiente** | `LastMatchPrices` consulta una tabla que ya no existe |

## Orden de lectura recomendado

1. `cmd/supercomparator/main.go` — Flags, semilla de las cuatro cadenas y
   `runServe` (que también arma el trabajo de precios).
2. `cmd/supercomparator/crawl.go` — Los comandos `crawl index`, `crawl precios` y
   `smoke`.
3. `internal/server/server.go` — Las rutas de la API (y que no toca ninguna
   tienda).
4. `internal/catalog/catalog.go` — Qué ve la web: DTO, cadenas y frescura.
5. `internal/catalog/fase3.go` — La compra y los precios a mano: qué suma el
   total y de dónde sale cada precio.
6. `internal/store/lista.go` y `internal/store/manual.go` — Las dos tablas de la
   compra, con el precio resuelto en SQL.
7. `internal/catalog/indexer.go` — Del sitemap a `products`.
8. `internal/catalog/prices.go` — El pegamento entre los adaptadores y el bucle
   de precios.
9. `internal/chain/chain.go` — El puerto `Chain` y sus tipos.
10. `internal/chain/ahorramas/` — Adaptador sencillo (HTTP + JSON-LD), el mejor
    para empezar.
11. `internal/chain/mercadona/` o `internal/chain/alcampo/` — Adaptadores con
    navegador headless (`internal/chain/browser`).
12. `internal/store/migrations.go` — El esquema y los triggers de FTS5.
13. `internal/store/search.go` — Cómo se busca y cómo se ordenan los resultados.
14. `internal/store/queue.go`, `priceloop.go` y `priceritmo.go` — La cola, el bucle
    que la consume y el ritmo/backoff.
15. `internal/match/match.go` — Normalización y lematización.

## Reglas del proyecto

- **Se trabaja siempre con agentes.** Ninguna tarea se hace en solitario: reparte
  el trabajo entre subagentes y quédate con la integración. La forma concreta de
  hacerlo está en [AGENTS.md](AGENTS.md#trabaja-siempre-con-agentes-regla-del-proyecto).
- Un concepto por fichero; evitar ficheros de más de ~300 líneas. Nada de
  paquetes `utils`, `common` o `helpers`.
- Interfaces definidas en el consumidor (idioma Go).
- `match` es puro: se testea con fixtures, sin red ni base de datos.
- La web no habla con las tiendas: si un endpoint necesita un dato que no está
  en la base, se escribe un comando (`crawl`), no un handler.
- Identificadores en inglés; salida, docs y commits en español.
- `gofmt`, `go vet ./...` y `go test ./...` deben pasar antes de commitear (lo
  mismo que CI).