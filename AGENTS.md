# AGENTS.md — convenciones del proyecto

Proyecto hecho con opencode + IA. Este fichero guía a cualquier agente que
trabaje en el repositorio. Si algo aquí contradice al código, **manda el
código**: actualiza este fichero en el mismo cambio.

## Qué es

SuperComparator indexa los catálogos de **Mercadona, Ahorramas, DÍA y Alcampo**
en una base SQLite (`datos/catalogo.db`) y sirve una **API JSON** en
`127.0.0.1:8080` para buscar productos y comparar precios. Ya **no** hay lista
de la compra ni informes markdown: `lista.md`, `lista.ejemplo.md` y los
`datos/*.md` son restos de la versión anterior y **nada los lee ni los escribe**.
No borres código de Go por tu cuenta, pero tampoco los menciones como
funcionalidad: no existen. La SPA de Angular es la fase 2.

Documentos de referencia: `README.md` (uso),
[ARCHITECTURE.md](ARCHITECTURE.md) (diseño y decisiones),
[docs/modelo-datos.md](docs/modelo-datos.md) (esquema de la base),
[docs/diagrama-flujo.md](docs/diagrama-flujo.md) (flujo) y
`internal/store/migrations.go` (el esquema, en la verdad).

## Stack

- Go 1.27.1, módulo `github.com/miferdev/superComparator`.
- Scraping: `go-rod/rod` (Mercadona y Alcampo, headless) y `net/http` + JSON-LD
  (Ahorramas y DÍA). El Chromium se comparte en `internal/chain/browser`.
- Persistencia: SQLite puro Go (`modernc.org/sqlite`), sin cgo, WAL y
  `SetMaxOpenConns(1)`.
- CLI: `cobra`. Parseo: `goquery`, `encoding/json`, `encoding/xml`.
- API: `net/http` con `http.ServeMux` y métodos en la ruta (`GET /api/...`).
- **Textos de salida, docs y comentarios en español; identificadores en
  inglés.** Los DTO de `internal/catalog/catalog.go` exponen las claves JSON en
  español a propósito (`precioMedida`, `precioViejo`…): es la API pública.

## Comandos

Lo que corre CI (`.github/workflows/ci.yml`), en este orden:

```sh
test -z "$(gofmt -l .)"   # formato
go vet ./...
go test ./...
go build ./...
```

**Antes de dar cualquier cosa por terminada, ejecuta los cuatro.** Si un cambio
es solo de docs, basta con comprobar que no has tocado código y que
`go build ./... && go test ./...` sigue en verde.

Atajos del `Makefile`:

```sh
make test              # go test ./...
make test-integration  # tests de red (build tag integration), no corren en CI
make build             # bin/  ->  bin/supercomparator (inyecta versión y commit)
make serve             # la web del catálogo en local
make index             # crawl index
make up                # docker compose up --build
make vet / make fmt    # utilidades
```

`make run` es alias de `serve`. **No existen** `make check`, `make report`,
`make explain`, ni los comandos `report` y `explain`: eran de la versión de
informes y se fueron. Si un texto los menciona, está obsoleto.

Comandos del programa:

- `supercomparator` / `serve` (por defecto) — levanta la web **y arranca la cola
  de precios en segundo plano**.
- `crawl index [--max N]` — lee los sitemaps y guarda el catálogo (rápido).
- `crawl precios [--limit N]` — una pasada de la cola: descarga hasta N fichas y
  dice cuántas salieron con precio y cuántas fallaron. Es la forma de probar
  contra la web real sin esperar horas.
- `smoke --url cadena=url` — descarga una ficha de cada cadena, diagnóstico.
- `version`.

Config por env (`internal/config/config.go`): `SUPERCOMPARATOR_ADDR`,
`SUPERCOMPARATOR_DB`, `SUPERCOMPARATOR_CADENAS`, `SUPERCOMPARATOR_CP`,
`SUPERCOMPARATOR_BROWSER_BIN` (o `ROD_BROWSER_BIN`),
`SUPERCOMPARATOR_TIMEOUT_S`, `SUPERCOMPARATOR_LOG`. Las flags (`--addr`,
`--db`, `--browser-bin`, `--cadenas`, `--log`) sobreescriben el env.

Docker: `compose.yml` monta `./datos` en `/datos` (la base), publica el puerto
**solo en `127.0.0.1:8080`** y fija `user: "${UID}:${GID}"`. `docker compose up`
sin `--build` reutiliza la imagen anterior: usa `--build` (o `make up`).

## Arquitectura (regla de dependencias)

`cmd → server → catalog → chain|match|store`, en un solo sentido, sin ciclos:

- `server` solo importa `catalog`, `store` y `version`. **La API no habla con
  ninguna tienda**: si un handler necesita algo que no está en la base, se
  escribe un comando (`crawl`), no un endpoint que baje a la red. El SSE
  (`GET /api/eventos`) lee la cola de la base, que es donde vive.
- `catalog` es el único que conoce `chain` + `match` + `store` a la vez
  (`indexer.go` indexa, `catalog.go` consulta y traduce a DTO, `prices.go` es el
  trabajo de precios).
- **El worker de la cola está en `store` y no conoce ningún adaptador**: descarga
  con la interfaz `store.PriceFetcher` (`Fetch(ctx, chainID, url) →
  chain.Product`), que implementa `catalog` con los adaptadores que le pasa
  quien lo arma. `store` solo depende de los tipos de `chain`, no de las tiendas.
- `chain` es el **puerto** (`Chain`: `ID`, `Sitemap`, `Fetch`, y la extensión
  opcional `Lookup`) y no importa nada interno. Los adaptadores de cadena solo
  pueden importar `chain`, `browser`, `config` y `match`, y **no se conocen
  entre sí**: para añadir una tienda se crea `internal/chain/<id>/` y se engancha
  en `selectChains` (`cmd/supercomparator/main.go`) y en `catalogChains`.
- **`PriceLoop.WithOnFicha` existe porque `match` vive en `catalog`**: el bucle no
  sabe tokenizar y no debe aprenderlo; quien normaliza el nombre de la ficha para
  poder buscarlo es `catalog`, que es el único que importa `match`. No muevas esa
  normalización a `store` para «dejarlo todo junto»: rompe la regla de
  dependencias.
- **`selectChains` recibe la `config.Config` entera, a propósito.** Hay un aviso
  en el propio código: construía una config con solo el navegador, sin código
  postal ni timeout, y con eso Mercadona devolvía «producto no encontrado» en
  todas sus fichas sin explicar por qué (no fijaba la tienda ni esperaba al
  render). Si algún día hay que tocarlo, no inventes una config más pequeña.
- `match` es puro (sin red, sin base). Hoy solo se usa para `search_name` y las
  medidas (`match.Tokens`, `match.ParseMeasure`, `match.Normalize`).
  `Rank`, `Similarity`, `Evaluar` y `AutoThreshold` son maquinaria de la fase de
  lista de la compra: **solo tienen tests**, no los llames desde código nuevo sin
  una razón clara (y si esa razón es la comparación entre cadenas de la fase 2,
  es justo para lo que sirven).
- `store/prices.go` es **herencia** del esquema de la lista: `LastMatchPrices`
  consulta una tabla `matches` que ya no existe. No escribas ahí; si tocas el
  esquema, decide antes si eso se borra.
- Un concepto por fichero; evitar ficheros de más de ~300 líneas. Nada de
  paquetes `utils`, `common` o `helpers`. Interfaces en el consumidor.

## Reglas de scraping (importante)

- Solo rutas permitidas por el `robots.txt` de cada cadena:
  - Mercadona: `/sitemap.xml` y `/product/...`. **Nunca `/api`.** Fija el CP
    28032 una vez por sesión (cookie del navegador); por eso sus fichas van con
    navegador headless en vez de con una petición HTTP simple. Sus fichas solo
    salen si el adaptador recibe la `config.Config` **entera** (código postal y
    timeout incluidos): con una config hecha a mano devolvía «producto no
    encontrado» en todas.
  - Ahorramas: sitemaps y fichas de producto. **Nunca `/buscador`,
    `/Search-ShowAjax` ni `/Product-Variation`.**
  - DÍA: `/sitemap.xml` y `/p/{id}`. **Nunca `*/search?*`** (su robots lo prohíbe
    y sus páginas de categoría devuelven 404). Su precio es nacional: no hay
    código postal que fijar y por eso no usa navegador.
  - Alcampo: `/sitemaps/*` y `/products/*`.
- **User-Agent honesto** (`chain.UserAgent`, con enlace al repo) en las
  peticiones HTTP simples: Mercadona (su sitemap), DÍA y Alcampo. No imites el de
  un navegador para colarte. **Excepción conocida:**
  `internal/chain/ahorramas` define su propio UA de navegador
  (`ahorramas.userAgent`) y lo manda en todas sus peticiones, sitemaps y fichas
  incluidos; no lo extiendas a más adaptadores. Si lo cambias, que sea por
  `chain.UserAgent` y comprobando que sus fichas siguen viniendo.
- El ritmo es un dato por cadena, en la tabla `chains`: `pausa_segundos` y
  `concurrencia`. En el arranque, `store.SeedChains` **solo rellena lo vacío**:
  si alguien ajusta el ritmo o activa `precios_activos` a mano, un arranque o un
  `crawl index` no lo deshacen (hay test: `main_test.go`
  `TestSeedCatalogNoPisaLoAjustado`).
- **No se salta el WAF de Alcampo.** Sus fichas están detrás de un WAF que
  responde 403 a los clientes automatizados, así que la cadena viene con
  `precios_activos = 0`. Si algún día pasan, se activa el interruptor.
- Un fallo encadenado **no para el bucle**: el fallo se anota en la ficha que
  falló, con su espera de reintento, y el round-robin sigue con la siguiente
  cadena. Lo que aparta una tienda entera es el interruptor `precios_activos`
  (Alcampo), no un fallo: una cadena que falla no puede impedir que terminen las
  demás.

## Reglas de la base de datos

- La base es **`datos/catalogo.db`** (no `precios.db`: ese fichero es de la
  versión antigua) y **está en `.gitignore`**, con todo `datos/`. No la
  comitees ni copies ficheros `.db`, `-wal` o `-shm`.
- **El esquema solo cambia con una migración** nueva al final de
  `internal/store/migrations.go`, versionada y aplicada en su propia
  transacción (`schema_migrations`). No edites una migración ya aplicada. La 2
  (`cola de precios rescatable`) añade `price_queue.updated_at` y su índice
  `idx_queue_estado`: sin esa marca no hay forma de distinguir una ficha que se
  está descargando de una que se quedó a medias al morir el proceso
  (`last_error` no sirve: solo se escribe cuando algo falla).
- `products_fts` la mantienen **triggers**, no Go. Ni un `INSERT` en
  `products_fts` desde el código: un trigger lo borraría. Para indexar otro
  campo, migración nueva (tabla virtual + triggers + el `backfill` que rellene lo
  ya guardado).
- `products` se identifica por `UNIQUE(chain, url)` y `UpsertProducts` es
  idempotente: **reindexar no puede pisar `price`, `measure_price`, `available`
  ni `price_fetched_at`**, que son de `store.SetPrice`. Mantén el `ON CONFLICT`
  así.
- Las marcas de tiempo van como texto RFC3339 en UTC (`store.ts`), porque se
  comparan y ordenan en SQL.

## La cola de precios se reanuda sola

`price_queue` **es** el estado de la cola, en la base: si la cola estuviera en
memoria, cerrar el contenedor perdería horas de trabajo. El consumidor ya existe
(`internal/store/priceloop.go`); las reglas que se derivan de que viva en SQLite
siguen siendo las mismas:

- El trabajo que se consume va **siempre** a la base: `EnqueuePrices` al
  indexar, `NextQueuedChain` (o `NextQueued` para varias cadenas) para reservar
  (marca `descargando` en la misma transacción, para que dos workers no peleen
  por la misma ficha), `MarkQueueDone` al terminar (borra la fila),
  `FailQueue` con `attempts` y `next_attempt_at` para el backoff y `Requeue`
  para rearmar a mano una ficha en `error`.
- `PriceLoop` no guarda trabajo entre llamadas: `Run(ctx, limit)` hace **una
  pasada** (cota de fichas por cadena) y `RunUntilEmpty(ctx)` las repite hasta
  que no queda nada pendiente ni reintentable. `serve` usa el segundo, en segundo
  plano; `crawl precios --limit N` usa el primero, para probar contra la web real
  sin esperar horas.
- El ritmo sale de `chains`, no de constantes (`internal/store/priceritmo.go`):
  cada flujo de cada cadena duerme su `pausa_segundos` y la cola se reparte por
  turnos entre las cadenas (`NextQueuedChain`), para que el catálogo no se llene
  de una sola tienda mientras las demás esperan.
- **El bucle solo trabaja las cadenas que se le pasaron, y tienen que ser
  exactamente las que tienen adaptador.** La lista la pasa quien lo arma
  (`catalog.NewPricesJob`), no la lee entera de la base: si preguntara por una
  tienda que no conoce, leería su cola, pediría esa ficha y la marcaría como
  fallida («cadena desconocida») sin motivo. Lo que sí sale de la base es
  `precios_activos`: Alcampo se queda fuera y sus fichas siguen en la cola, que
  es justo donde deben estar (ver `activas()`).
- Si el proceso muere, `ReclaimStale(ttl)` devuelve a `pendiente` lo que quedó en
  `descargando` (ttl = un minuto, `reclaimTTL`); el bucle lo hace **una sola vez
  por proceso** y **sin tocar los intentos**: el rescate es por trabajo perdido,
  no por un fallo de la tienda, y sumarlos dejaría fichas buenas en `error`.
- `MaxIntentos` (5) es el tope: a partir de ahí la ficha queda en `error` y
  espera a que alguien la rearme. Ni un 404 (`chain.ErrNotFound`) ni los intentos
  ya agotados se arreglan reintentando, así que se aparcan de una en `error`.
  Un fallo normal solo espera su backoff (30 s doblando hasta 5 min, en
  `priceritmo.go`) y la pasada sigue.
- La web lo ve por `GET /api/eventos` (Server-Sent Events): un evento `estado`
  cada 2 s con, por cadena, `pendiente`, `descargando`, `error` y `precios`. Es
  un stream que se abre una vez; lee la cola de la base, no al bucle.

## Un producto dudoso no entra en la comparativa

Es la regla que más se ha roto en versiones anteriores, así que va explícita:
**nada de lo que no venga de la fuente se da por bueno.**

- Si la ficha no dio precio, `price` se queda en `0` y el producto se sirve con
  `tienePrecio: false`. No se rellena con el precio de otra tienda, ni con el
  de hace días haciéndolo pasar por actual, ni con un precio estimado.
- **Un `0` en `price` no siempre es un dato que falte.** Si la tienda **solo**
  publica €/kg o €/l (producto vendido al peso, como los plátanos), `Price` y
  `UnitPrice` quedan a `0` **a propósito** y el dato va en
  `MeasurePrice`/`MeasureUnit`. Lo dice el propio producto con
  `PriceIsPerMeasure` y su método `PrecioEsPorMedida()`: quien llama no tiene que
  deducirlo comparando números. La API lo expone como `precioSoloMedida`.
- `price_basis` describe **en qué base está el precio guardado en `products.price`**,
  que es el que se enseña y el que se suma al total. Si hay precio de unidad, la
  base es `unidad` aunque además haya €/kg: una botella de vino a 3,65 € que
  además está a 4,87 €/l sigue siendo una botella. La base solo es la medida
  cuando no hay precio de unidad.
- `SetPrice` guarda `measure_unit` en `products` (no solo en `price_history`),
  porque el DTO la lee de ahí.
- Si el sitemap no trae el nombre (DÍA solo da la categoría), se marca
  `name_source = categoria` y `crawl_state = ficha_pendiente` para que la cola
  lo visite; **no** se inventa un nombre ni se presenta la categoría como si
  fuera el producto.
- `store.FichaData` escribe el nombre, `search_name`, la categoría, pone
  `name_source = 'ficha'` y saca el producto de `crawl_state =
  'ficha_pendiente'`. Es lo que arregla DÍA, cuyos productos se llamaban «Leche»
  y no aparecían al buscar «leche entera». **Solo escribe lo que viene
  informado**, para no degradar lo que ya estaba bien.
- Cuando llegue la comparación entre cadenas (fase 2), se apoya en
  `match.Similarity`/`Evaluar` y su umbral, y por debajo del umbral el producto
  **no** entra en la comparativa: se queda pendiente de revisión. Como ya no hay
  informes, la traducción es «no aparece como ganador ni en subtotales».
- Lo mismo vale para las medidas: si la medida no está publicada, `measure_price`
  es 0 y no se deduce del nombre de la categoría.

## Desescapar y parsear: cada formato con su función

Dos confusiones que costaron tiempo y que no dan error, solo datos malos:

- **`chain.UnescapeText` solo para el texto crudo del JSON-LD.** El contenido de
  un `<script type="application/ld+json">` es texto plano para `net/html`, así que
  un `Hellmann&#039;s` llega entero al nombre y el usuario no lo encuentra
  buscando el apóstrofo. El texto que sale del DOM (`goquery .Text()`) **ya**
  viene desescapado por el parser de HTML: volver a desescaparlo deshace un
  `&amp;lt;` que en la ficha quería decir `&lt;` y acaba mostrando `<`.
- **El precio del JSON-LD se lee con `chain.ParseJSONNumber`** (formato máquina,
  punto decimal), **nunca con `chain.ParsePrice`** (formato español, coma).
  Confundirlas no da error: da un número plausible pero equivocado, así que
  `1.65` se guardaba como `1` y cualquier producto por debajo de 1 € se quedaba
  **sin precio**. Hay un test que lo fija (`TestPreciosNoSonIntercambiables` en
  `internal/chain/price_test.go`): si algún día se toca una de las dos funciones,
  ese test obliga a decidir a propósito cuál se usa en cada sitio.

## Trabaja siempre con agentes (regla del proyecto)

**Ninguna tarea de este repositorio se hace en solitario: reparte el trabajo
entre subagentes y quédate con la integración.** Es una decisión del proyecto, no
una preferencia, así que se aplica igual a tareas pequeñas.

Cómo:

1. **Antes de tocar nada, parte el trabajo** en trozos que no se pisen. Lo
   habitual es lanzar en paralelo un agente por paquete (`internal/store/`,
   `internal/chain/`, `internal/server/`…), y reserva para ti las piezas de
   enganche: el dominio (`internal/catalog/`), el CLI y la verificación final.
2. **El prompt del agente va con el contrato exacto**: ficheros que puede tocar,
   firmas de funciones que debe respetar, casos de test que quieres y los tres
   comandos con los que tiene que terminar en verde (`gofmt`, `go vet`,
   `go test` de **su** paquete). Explícale el porqué cuando el código no lo diga.
3. **Los agentes se pisan si les das los mismos ficheros.** Da a cada uno un
  directorio propio y dilo. Un agente no debe ejecutar `go test ./...` si otro
   está editando otro paquete: dale su paquete.
4. **Revisa y corrige lo que devuelvan.** Un agente que termina no significa
   que esté bien: léete el diff, ejecuta tú los tests y arregla lo que se haya
   equivocado (nombres, firmas, expectativas del test).
5. **No te fíes de los números.** Que diga "funciona" no es lo mismo que
   funcionar; verifícalo tú con `go build`, `go vet`, `go test` y, cuando toque,
   contra el mundo real.

La documentación también se delega: es un trabajo mecánico y paralelo. Si aun
así no puedes usar agentes (por ejemplo, si la tarea es una sola edición), al
menos deja constancia en el resumen del cambio de que se hizo a mano y por qué.

## Convenciones de código

- `gofmt`, `go vet ./...` y `go test ./...` deben pasar **antes de commitear**
  (es lo que corre CI). Un test solo con red va en
  `internal/chain/*/integration_test.go` con `//go:build integration`; los
  fixtures HTML reales de las fichas están en `testdata/` (Alcampo no tiene: su
  WAF no deja guardar una).
- Sin comentarios salvo que aporten intención. Comentarios y errores de
  usuario, en español.
- No añadas dependencias sin motivo escrito: el árbol es deliberadamente
  pequeño (cobra, rod, goquery, modernc.org/sqlite, x/text).
- Tests: tabla de casos cuando haya varios, y aserta el comportamiento
  observable (lo que sale por la API o lo que queda en la base), no los
  detalles internos.

## Lo que no existe todavía (no lo prometas ni lo des por hecho)

La SPA de Angular, la recomprobación de precios viejos al abrir un producto, la
vista «el mismo producto en otras tiendas», «Mi lista» con subtotales
(`list_items` está creada y vacía, sin endpoints) y el reanudar del indexado
desde `crawl_state` (hoy `crawl index` relee el sitemap entero; que sea
idempotente es lo que lo hace seguro). La tabla de fases está en
[ARCHITECTURE.md](ARCHITECTURE.md).