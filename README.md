# SuperComparator

Catálogo de precios de **Mercadona, Ahorramas, DÍA y Alcampo**. El programa
indexa los catálogos de las cuatro tiendas en una base SQLite y sirve una **API
JSON** para buscar productos, ver su precio por unidad y por peso (€/kg, €/L) y
comparar entre tiendas. Dentro de poco habrá una SPA de Angular encima de esa
misma API (fase 2); hoy la web es una página mínima que la lista.

> Esto **ya no** es un comparador de listas de la compra. No existe `lista.md`, ni
> se escriben informes markdown, ni se resuelve una compra automáticamente: el
> usuario navega el catálogo y compara él. Los ficheros `datos/*.md` que veas en
> tu máquina son restos de la versión anterior y no los genera nada.

## Estado actual

Indexado con `crawl index` contra las cuatro tiendas (106.226 productos, base de
82 MB):

| Cadena    | Productos | Precios activos | Nota |
|-----------|----------:|-----------------|------|
| Mercadona | 4.320     | sí              | El nombre sale del slug, pero **la medida no** (aparece al visitar la ficha) |
| Ahorramas | 4.536     | sí              | Nombre y medida salen del slug (3.039 con medida) |
| DÍA       | 7.755     | sí              | Su sitemap **no trae el nombre**, solo la categoría; se marca `ficha_pendiente` para que la cola le ponga el nombre real |
| Alcampo   | 89.615    | **no**          | Su WAF responde 403 a las fichas, así que entra el catálogo pero no el precio (el nombre y la medida sí salen del slug) |

Dos cosas que conviene no confundir:

- **«Precios activos»** es el interruptor por cadena (`precios_activos` en la
  tabla `chains`): dice si la cola de precios podrá visitar esas fichas. Alcampo
  lo tiene apagado a propósito, no porque su catálogo falte.
- **Precio guardado** hoy es `0` en las cuatro cadenas. Todavía no hay nadie que
  descargue fichas: falta el worker de la cola de precios (ver
  [qué no existe todavía](#qué-no-existe-todavía)). El catálogo y la búsqueda ya
  funcionan; los precios todavía no.

La búsqueda con FTS5 responde en **4-26 ms** sobre las 106.000 filas. El
tokenizador le quita las tildes, así que escribir «fresas» (plural, sin tilde)
encuentra «fresas bandeja»; además se indexa `search_name`, el nombre sin
stopwords ni plurales, para que «fresa» encuentre lo mismo.

## Puesta en marcha (Docker)

```sh
docker compose up --build
```

Y abre <http://127.0.0.1:8080>. El contenedor arranca la web del catálogo y deja
la API en marcha. `--build` importa: sin él se reutiliza la imagen anterior y
puedes estar viendo una versión vieja. `make up` ya lo incluye.

El puerto está publicado solo en `127.0.0.1`, así que la web no sale del
ordenador. Para abrirla desde el móvil tendrías que cambiar el mapeo a
`8080:8080` en `compose.yml` (con el aviso de que cualquiera de tu wifi podría
entrar).

## Llenar el catálogo

La web no indexa sola: el indexado es un comando aparte, y es la parte rápida
(son sitemaps, no fichas).

```sh
docker compose run --rm app crawl index
```

Lee el sitemap de cada cadena, guarda nombre, medida, categoría y enlace de cada
producto en `datos/catalogo.db`, encola en `price_queue` lo que aún no tiene
precio y termina con un resumen:

```
Cadena        En catálogo   Con precio   Sin precio
ahorramas          4536           0         4536
alcampo           89615           0        89615
dia                7755           0         7755
mercadona          4320           0         4320
```

Para probar sin llenar 106.000 filas:

```sh
docker compose run --rm app crawl index --max 500
```

Se puede repetir tantas veces como quieras: el `upsert` es idempotente sobre
`(cadena, url)`, así que no duplica nada **ni pisa los precios** que ya tengas.

## Comandos

| Comando | Qué hace |
|---------|----------|
| `supercomparator` o `supercomparator serve` | Levanta la web del catálogo (es lo que hace por defecto) |
| `supercomparator crawl index [--max N]` | Indexa los catálogos desde los sitemaps (`--max` acota para probar) |
| `supercomparator smoke --url cadena=url` | Descarga una ficha de cada cadena: diagnóstico de un scraper roto |
| `supercomparator version` | Versión con la que se compiló |

Con Docker, `app crawl index --max 500` es el equivalente de
`docker compose run --rm app crawl index --max 500`.

Diagnóstico de una ficha concreta (por ejemplo, para ver si Mercadona sigue
devolviendo el HTML que esperamos):

```sh
docker compose run --rm app smoke \
  --url mercadona=https://tienda.mercadona.es/product/3723/fresas-bandeja
```

## API

Todo cuelga de `http://127.0.0.1:8080`. Devuelve JSON y no habla nunca con una
tienda: solo lee y escribe en la base.

| Ruta | Qué devuelve |
|------|--------------|
| `GET /api/estado` | Versión, las cuatro cadenas con su recuento y el último trabajo |
| `GET /api/cadenas` | Las cuatro tiendas: total, con precio, sin precio, fase, pausada, precios activos |
| `GET /api/catalogo?q=&cadena=&conPrecio=&orden=&offset=&limite=` | Búsqueda paginada |
| `GET /api/producto?cadena=&url=` | Un producto por cadena y URL |
| `GET /api/version` | Versión |
| `GET /` | Página HTML mínima que lista la API (la SPA llega en la fase 2) |

Parámetros de `/api/catalogo`: `q` (texto), `cadena` (`mercadona`, `ahorramas`,
`dia`, `alcampo`), `conPrecio` (`1`), `orden` (`relevancia` —por defecto—,
`precio_asc`, `precio_desc`, `nombre`, `medida_asc`), `offset` y `limite`
(por defecto 50, tope 200).

```sh
# Cómo va el catálogo entero
curl -s http://127.0.0.1:8080/api/cadenas

# Buscar "fresas" solo en Mercadona
curl -s 'http://127.0.0.1:8080/api/catalogo?q=fresas&cadena=mercadona&limite=2'

# Lo mismo, ordenando por precio y quedándote solo con lo que tiene precio
curl -s 'http://127.0.0.1:8080/api/catalogo?q=fresas&conPrecio=1&orden=precio_asc'

# Un producto concreto (la URL hay que codificarla)
curl -s --get http://127.0.0.1:8080/api/producto \
  --data-urlencode 'cadena=mercadona' \
  --data-urlencode 'url=https://tienda.mercadona.es/product/3723/fresas-bandeja'
```

`GET /api/catalogo` devuelve `{productos, total, hayMas}`. Cada producto trae
`precio`, `precioBase` (`unidad` o `kg`: así lo publica la tienda),
`precioMedida` (por kg o l, que es lo comparable), `medidaValor`,
`medidaUnidad`, `url` (enlace a la ficha), `tienePrecio`, `precioViejo` y
`frescuraHoras`:

```json
{
  "id": 2281,
  "cadena": "mercadona",
  "nombreCadena": "Mercadona",
  "url": "https://tienda.mercadona.es/product/3723/fresas-bandeja",
  "nombre": "fresas bandeja",
  "sku": "3723",
  "formato": "",
  "medidaValor": 0,
  "medidaUnidad": "",
  "categoria": "",
  "precio": 0,
  "precioBase": "",
  "precioMedida": 0,
  "disponible": true,
  "precioComprobado": "0001-01-01T00:00:00Z",
  "nombreFuente": "slug",
  "tienePrecio": false,
  "precioViejo": false,
  "frescuraHoras": 0
}
```

Ese `precio: 0` es honesto, no un fallo: el catálogo está indexado pero la ficha
todavía no se ha visitado. Cuando lo esté, vendrán `precio`, `precioMedida` y
`precioComprobado` con la fecha; `precioViejo` se pone a `true` cuando el precio
pasa de `precioMaxHoras` (24 h por defecto) y es la señal de que hay que
recomprobarlo.

## Configuración

Todo se ajusta por variables de entorno; las flags las sobreescriben.

| Variable | Por defecto | Descripción |
|----------|-------------|-------------|
| `SUPERCOMPARATOR_ADDR` | `127.0.0.1:8080` | Dirección donde escucha la web (en Docker, `0.0.0.0:8080`) |
| `SUPERCOMPARATOR_DB` | `datos/catalogo.db` | Ruta de la base SQLite |
| `SUPERCOMPARATOR_CADENAS` | (todas) | Cadenas a usar, separadas por comas: `mercadona,ahorramas,dia,alcampo` |
| `SUPERCOMPARATOR_CP` | `28032` | Código postal que se fija en Mercadona |
| `SUPERCOMPARATOR_BROWSER_BIN` | (auto) | Binario de Chromium para Mercadona y Alcampo; si no, se lee `ROD_BROWSER_BIN` |
| `SUPERCOMPARATOR_TIMEOUT_S` | `40` | Tiempo máximo de espera por petición, en segundos |
| `SUPERCOMPARATOR_LOG` | (vacío) | Fichero de log; si se deja vacío los logs se descartan |

Flags (válidas en todos los subcomandos): `--addr`, `--db`, `--browser-bin`,
`--cadenas`, `--log`. Además, `crawl index` acepta `--max N` y `smoke` acepta
`--url cadena=url`.

Sin Docker hace falta Go 1.27 y un Chromium/Chrome en el `PATH` (si no,
`SUPERCOMPARATOR_BROWSER_BIN=/ruta/a/chrome`):

```sh
make serve                 # o: go run ./cmd/supercomparator serve
make index                 # o: go run ./cmd/supercomparator crawl index
```

## Las cuatro cadenas

| Cadena | Cómo lee el catálogo | Cómo lee la ficha | Notas |
|--------|----------------------|-------------------|-------|
| Mercadona | `/sitemap.xml` por HTTP | Navegador headless (`/product/...`) | Fija el CP 28032 una vez por sesión. El slug trae el nombre pero **no** la medida |
| Ahorramas | `sitemap_index.xml` + sitemaps de producto por HTTP | HTTP, JSON-LD | Nombre y medida salen del slug |
| DÍA | `/sitemap.xml` por HTTP | HTTP, JSON-LD (`/p/{id}`) | Su sitemap solo da la **categoría** |
| Alcampo | `/sitemaps/*` por HTTP | Navegador headless (`/products/...`) | Su WAF responde 403 a las fichas |

**DÍA.** El sitemap publica rutas como
`/aceites-salsas-y-especias/aceites/p/100`: no hay nombre de producto, solo la
sección. Sus 7.755 filas nacen con `crawl_state = ficha_pendiente` y
`name_source = categoria`, y es la cola de precios la que tiene que visitarlas
para escribir el nombre real que publica la ficha. Hasta entonces, lo que se
busca en DÍA son categorías («leche», «vino tinto»), no productos concretos.

**Alcampo.** Su catálogo se lee muy bien (89.615 productos), pero las fichas están
detrás de un WAF que responde 403 a los clientes automatizados, así que no sale
ningún precio. De ahí que venga con `precios_activos = 0`. **No se intenta
saltarse ese WAF.**

## Qué no existe todavía

Para no prometer nada que no esté en el repo:

- **La SPA de Angular.** Hoy `GET /` devuelve una página HTML mínima que lista la
  API. La API está lista; la web no.
- **La cola de precios.** Es lo que rellena los precios: falta el worker que
  consume `price_queue`. Sí están escritos el estado en SQLite y las operaciones
  de `store` (`EnqueuePrices`, `NextQueued`, `FailQueue`, `MarkQueueDone`), así
  que cuando se escriba no habrá que rediseñar nada. Como no hay quien consuma la
  cola, hoy ningún producto tiene precio y `price_history` sigue vacía.
- **La comparación «en otras tiendas».** No hay ninguna vista que agrupe el mismo
  producto entre cadenas: la comparación la hace la persona, producto a producto,
  con `precioMedida` a la vista.
- **«Mi lista».** La tabla `list_items` existe y está vacía, pero no hay
  endpoints ni nada que la use: sin añadir productos, sin subtotales ni total.
- Tampoco hay: notificaciones por SSE, recomprobación de un precio viejo al
  abrirlo, ni consulta del histórico de cambios por la API (`store.Changes` existe
  en el código, pero ningún endpoint lo llama).

## Scraping responsable

- **Solo rutas permitidas por el `robots.txt` de cada cadena**: sus sitemaps y las
  fichas de producto. Nunca sus APIs internas ni sus buscadores. Concreto:
  - Mercadona: `/sitemap.xml` y `/product/...`. **Nunca `/api`.**
  - Ahorramas: sitemaps y fichas. **Nunca `/buscador`, `/Search-ShowAjax` ni
    `/Product-Variation`.**
  - DÍA: `/sitemap.xml` y `/p/{id}`. **Nunca `*/search?*`** (su robots lo prohíbe
    y sus páginas de categoría devuelven 404).
  - Alcampo: `/sitemaps/*` y `/products/*`.
- **User-Agent honesto** en las peticiones HTTP simples:
  `supercomparator/0.1 (+https://github.com/miferdev/superComparator)`. Nunca se
  imita el de un navegador para colarse. (Excepción conocida y documentada: el
  adaptador de Ahorramas manda un UA de navegador; si algún día se arregla, que
  sea con el UA honesto y comprobando que sus fichas siguen viniendo.)
- **Ritmo bajo y por cadena.** Cada tienda tiene su pausa y su concurrencia
  configuradas en la tabla `chains` (Mercadona 2 s y 1 a la vez; Ahorramas y DÍA
  1,5 s y 2; Alcampo 3 s y 1), y un fallo encadenado pausa esa cadena sola,
  mientras las demás siguen.
- **No se salta el WAF de Alcampo.** Si su web deja pasar las fichas, se
  activa; mientras tanto, solo su catálogo.
- Proyecto personal y educativo. Las webs cambian y el scraper tendrá que
  adaptarse; si una tienda cambia sus sitemaps, se toca su adaptador, no se
  fuerza la ruta.

## Desarrollo

```sh
make test              # tests unitarios
make test-integration  # tests con red (build tag integration; no corren en CI)
make build             # bin/  ->  bin/supercomparator
make serve             # la web del catálogo en local
make index             # indexa los catálogos
make up                # docker compose up --build
```

Antes de commitear, lo mismo que CI: `gofmt` limpio, `go vet ./...` y
`go test ./...` en verde.

Quien trabaje en este repositorio (persona o agente de IA) **reparte el trabajo
entre subagentes y se queda con la integración**; nunca hace una tarea en
solitario. Las reglas concretas están en
[AGENTS.md](AGENTS.md#trabaja-siempre-con-agentes-regla-del-proyecto).

## Documentación

- [ARCHITECTURE.md](ARCHITECTURE.md) — mapa de paquetes, regla de dependencias,
  decisiones (SQLite, FTS5, la cola en base) y estado de cada fase.
- [docs/modelo-datos.md](docs/modelo-datos.md) — diagrama entidad-relación y
  diccionario de datos de `datos/catalogo.db`.
- [docs/diagrama-flujo.md](docs/diagrama-flujo.md) — arranque, indexado, cola de
  precios y recorrido en la web.
- [AGENTS.md](AGENTS.md) — convenciones del repo para quien trabaje en él.

## Licencia

MIT.