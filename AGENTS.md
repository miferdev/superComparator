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

- `supercomparator` / `serve` (por defecto) — levanta la web.
- `crawl index [--max N]` — lee los sitemaps y guarda el catálogo (rápido).
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
  escribe un comando (`crawl`), no un endpoint que baje a la red.
- `catalog` es el único que conoce `chain` + `match` + `store` a la vez
  (`indexer.go` indexa, `catalog.go` consulta y traduce a DTO).
- `chain` es el **puerto** (`Chain`: `ID`, `Sitemap`, `Fetch`, y la extensión
  opcional `Lookup`) y no importa nada interno. Los adaptadores de cadena solo
  pueden importar `chain`, `browser`, `config` y `match`, y **no se conocen
  entre sí**: para añadir una tienda se crea `internal/chain/<id>/` y se engancha
  en `selectChains` (`cmd/supercomparator/main.go`) y en `catalogChains`.
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
    navegador headless en vez de con una petición HTTP simple.
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
- Un fallo encadenado pausa **esa** cadena; las demás siguen. Una cadena que
  falla no puede impedir que terminen las demás (`IndexCatalog`).

## Reglas de la base de datos

- La base es **`datos/catalogo.db`** (no `precios.db`: ese fichero es de la
  versión antigua) y **está en `.gitignore`**, con todo `datos/`. No la
  comitees ni copies ficheros `.db`, `-wal` o `-shm`.
- **El esquema solo cambia con una migración** nueva al final de
  `internal/store/migrations.go`, versionada y aplicada en su propia
  transacción (`schema_migrations`). No edites una migración ya aplicada.
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
memoria, cerrar el contenedor perdería horas de trabajo. Reglas que se derivan:

- El trabajo que se consume va **siempre** a la base: `EnqueuePrices` al
  indexar, `NextQueued` para reservar (marca `descargando` en la misma
  transacción, para que dos workers no peleen por la misma ficha),
  `MarkQueueDone` al terminar, `FailQueue` con `attempts` y `next_attempt_at`
  para el backoff.
- El ritmo sale de `chains`, no de constantes en el código.
- Ningún arranque reconstruye nada desde memoria: si el proceso muere, se
  reanuda leyendo las tablas.
- Ojo: el **worker que consume la cola todavía no existe** (fase 2). Lo que
  existe es el estado y las operaciones de `store`. Si lo escribes, que sea
  reanudable desde cero y respete `precios_activos`.

## Un producto dudoso no entra en la comparativa

Es la regla que más se ha roto en versiones anteriores, así que va explícita:
**nada de lo que no venga de la fuente se da por bueno.**

- Si la ficha no dio precio, `price` se queda en `0` y el producto se sirve con
  `tienePrecio: false`. No se rellena con el precio de otra tienda, ni con el
  de hace días haciéndolo pasar por actual, ni con un precio estimado.
- Si el sitemap no trae el nombre (DÍA solo da la categoría), se marca
  `name_source = categoria` y `crawl_state = ficha_pendiente` para que la cola
  lo visite; **no** se inventa un nombre ni se presenta la categoría como si
  fuera el producto.
- Cuando llegue la comparación entre cadenas (fase 2), se apoya en
  `match.Similarity`/`Evaluar` y su umbral, y por debajo del umbral el producto
  **no** entra en la comparativa: se queda pendiente de revisión. Como ya no hay
  informes, la traducción es «no aparece como ganador ni en subtotales».
- Lo mismo vale para las medidas: si la medida no está publicada, `measure_price`
  es 0 y no se deduce del nombre de la categoría.

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

La SPA de Angular, el worker de la cola de precios, la recomprobación de precios
viejos y el SSE, la vista «el mismo producto en otras tiendas», «Mi lista» con
subtotales (`list_items` está creada y vacía, sin endpoints) y el reanudar del
indexado desde `crawl_state` (hoy `crawl index` relee el sitemap entero; que sea
idempotente es lo que lo hace seguro). La tabla de fases está en
[ARCHITECTURE.md](ARCHITECTURE.md).