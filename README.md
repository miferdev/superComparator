# SuperComparator

Catálogo de precios de **Mercadona, Ahorramas, DÍA y Alcampo**. El programa
indexa los catálogos de las cuatro tiendas en una base SQLite y sirve una **API
JSON** para buscar productos, ver su precio por unidad y por peso (€/kg, €/L),
comparar entre tiendas y llevar una **«Mi compra»** con subtotales y total. Dentro
de poco habrá una SPA de Angular encima de esa misma API (fase 2); hoy la web es
una página mínima que la lista.

> Esto **ya no** es un comparador automático de listas de la compra: no existe
> `lista.md`, no se escriben informes markdown y **no se resuelve ni se optimiza
> una compra**. La persona navega el catálogo, compara y elige. Lo que sí hay es
> una lista propia para apuntar lo que va a comprar y ver cuánto suma, y precios
> que puedes escribir a mano cuando la tienda no los da. Los ficheros `datos/*.md`
> que veas en tu máquina son restos de la versión anterior y no los genera nada.

## Estado actual

Indexado con `crawl index` contra las cuatro tiendas (106.226 productos, base de
82 MB):

| Cadena    | Productos | Precios activos | Nota |
|-----------|----------:|-----------------|------|
| Mercadona | 4.320     | sí              | El nombre sale del slug, pero **la medida no** (aparece al visitar la ficha) |
| Ahorramas | 4.536     | sí              | Nombre y medida salen del slug (3.039 con medida) |
| DÍA       | 7.755     | sí              | Su sitemap **no trae el nombre**, solo la categoría; se marca `ficha_pendiente` para que la cola le ponga el nombre real |
| Alcampo   | 89.615    | **no**          | Su WAF responde 403 a las fichas, así que entra el catálogo pero no el precio (el nombre y la medida sí salen del slug). El precio lo pones tú a mano, con `/api/precios-manuales` |

Dos cosas que conviene no confundir:

- **«Precios activos»** es el interruptor por cadena (`precios_activos` en la
  tabla `chains`): dice si la cola de precios podrá visitar esas fichas. Alcampo
  lo tiene apagado a propósito, no porque su catálogo falte.
- **Precio guardado** depende de cuánto haya corrido la cola. El worker existe y
  `serve` lo arranca en segundo plano al levantar la web, así que acaba
  llenándose solo; si acabas de indexar, lo normal es seguir viendo `precio: 0`
  en todo: no es un fallo, es que la cola todavía no ha llegado a esas fichas. Se
  ve en `GET /api/cadenas` y en vivo en `GET /api/eventos`.

La búsqueda con FTS5 responde en **4-26 ms** sobre las 106.000 filas. El
tokenizador le quita las tildes, así que escribir «fresas» (plural, sin tilde)
encuentra «fresas bandeja»; además se indexa `search_name`, el nombre sin
stopwords ni plurales, para que «fresa» encuentre lo mismo.

## Puesta en marcha (Docker)

```sh
docker compose up --build
```

Y abre <http://127.0.0.1:8080>. El contenedor arranca la web del catálogo, deja
la API en marcha y echa la cola de precios en segundo plano (va rellenando
precios mientras navegas). `--build` importa: sin él se reutiliza la imagen
anterior y puedes estar viendo una versión vieja. `make up` ya lo incluye.

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

Esos ceros en «Con precio» son los de un catálogo recién indexado: la cola de
precios va rellenándolos después (siguiente sección).

Para probar sin llenar 106.000 filas:

```sh
docker compose run --rm app crawl index --max 500
```

Se puede repetir tantas veces como quieras: el `upsert` es idempotente sobre
`(cadena, url)`, así que no duplica nada **ni pisa los precios** que ya tengas.

## Rellenar los precios

Descargar fichas es la parte lenta: respeta el ritmo de cada tienda (pausa y
concurrencia están en la tabla `chains`), así que 100.000 fichas son horas. El
trabajo se guarda en la base, en `price_queue`, de modo que se puede parar y
seguir: al arrancar, `serve` arranca la cola **en segundo plano** y la web
responde mientras tanto. Si el proceso muere, al siguiente arranque se reanuda
sola.

Para verlo sin esperar horas, una pasada acotada desde la línea de comandos:

```sh
# 20 fichas, y al terminar dice cuántas salieron con precio y cuántas fallaron
docker compose run --rm app crawl precios --limit 20

# solo una tienda, para depurar su scraper
docker compose run --rm app crawl precios --limit 5 --cadenas dia
```

Y para seguir el progreso sin preguntar cada poco:

```sh
# stream: un evento 'estado' cada 2 segundos, se corta con Ctrl-C
curl -N http://127.0.0.1:8080/api/eventos
```

Sale una línea por evento con, por cadena, `pendiente`, `descargando`, `error` y
`precios`:

```
event: estado
data: {"cadenas":[{"cadena":"mercadona","nombre":"Mercadona","pendiente":4289,"descargando":1,"error":3,"precios":28}],"cuando":"2026-10-02T09:14:05Z"}
```

Alcampo no aparece avanzando porque viene con `precios_activos = 0`: sus fichas
siguen en la cola, que es justo donde deben estar.

## Comandos

| Comando | Qué hace |
|---------|----------|
| `supercomparator` o `supercomparator serve` | Levanta la web del catálogo y arranca la cola de precios en segundo plano (es lo que hace por defecto) |
| `supercomparator crawl index [--max N]` | Indexa los catálogos desde los sitemaps (`--max` acota para probar) |
| `supercomparator crawl precios [--limit N]` | Una pasada de la cola de precios: descarga hasta N fichas y dice cuántas salieron con precio y cuántas fallaron |
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
| `GET /api/eventos` | Server-Sent Events con el estado de la cola de precios (un evento `estado` cada 2 s) |
| `GET /api/mi-compra` | La compra: líneas, subtotal por tienda, total, `sinPrecio` y `avisoTotal` |
| `POST /api/mi-compra` | Añade un producto; 201 con la lista ya actualizada |
| `PUT /api/mi-compra` | Cambia la cantidad (`cantidad` 0 o menor lo quita) |
| `DELETE /api/mi-compra?cadena=&url=` | Quita un producto; sin parámetros, vacía la lista |
| `GET /api/precios-manuales` | Todos los precios puestos a mano, con el nombre del producto |
| `GET /api/precios-manuales?cadena=&url=` | Uno, o 404 si esa ficha no tiene precio a mano |
| `PUT`/`POST /api/precios-manuales` | Guarda un precio a mano (`POST` es alias de `PUT`) |
| `DELETE /api/precios-manuales?cadena=&url=` | Quita el precio a mano de una ficha |
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

# Seguir la cola de precios en vivo (Ctrl-C para cortarlo)
curl -N http://127.0.0.1:8080/api/eventos
```

`GET /api/catalogo` devuelve `{productos, total, hayMas}`. Cada producto trae
`precio`, `precioBase` (`unidad` o `kg`: así lo publica la tienda),
`precioMedida` (por kg o l, que es lo comparable), `medidaValor`,
`medidaUnidad`, `url` (enlace a la ficha), `tienePrecio`, `precioSoloMedida`,
`precioFuente`, `precioViejo` y `frescuraHoras`:

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
  "precioSoloMedida": false,
  "precioFuente": "ninguno",
  "precioViejo": false,
  "frescuraHoras": 0
}
```

Ese `precio: 0` es honesto, no un fallo: el catálogo está indexado pero la ficha
todavía no se ha visitado. Cuando lo esté, vendrán `precio`, `precioMedida` y
`precioComprobado` con la fecha; `precioViejo` se pone a `true` cuando el precio
pasa de `precioMaxHoras` (24 h por defecto) y es la señal de que hay que
recomprobarlo.

Un `precio: 0` con `precioSoloMedida: true` es otro caso: la tienda **solo**
publica €/kg o €/l (los plátanos, que se venden al peso), así que no hay precio de
unidad que guardar y el dato está en `precioMedida`. No es un dato que falte.

`precioFuente` dice de dónde sale **el precio de unidad** que se enseña y se suma
a un total: `web` (lo sacamos de la tienda), `manual` (lo puso el usuario, con
`/api/precios-manuales`) o `ninguno` (no hay precio de unidad). **Ojo con
`ninguno`**: no significa «no hay precio de ningún tipo», sino que no hay precio de
unidad. Un producto vendido al peso sale con `ninguno` aunque su €/kg venga de la
web, porque de un €/kg no sale el precio de una bolsa.

## Precios que pones tú

Alcampo está detrás de un WAF que responde 403 a los clientes automatizados: su
catálogo se lee (89.615 productos) pero de sus fichas no sale ningún precio, y no
se intenta saltarse ese WAF. Para saber cuánto costaría la compra ahí, escribes a
mano el precio que ves en la tienda. Lo mismo sirve para cualquier precio que hayas
visto en el lineal y no esté en la web.

Es **tu dato, no el de la tienda**, y por eso va a su propia tabla: guardar un
precio a mano no toca el precio que publica la web, ni al revés. Solo se puede
guardar para un producto que esté en el catálogo (si no, un 404 lo dice), y
`precioMedida` solo admite `kg` o `l`.

```sh
# Un producto de Alcampo a 1,25 € (la URL hay que codificarla)
curl -s -X PUT http://127.0.0.1:8080/api/precios-manuales \
  -H 'Content-Type: application/json' \
  --data '{"cadena":"alcampo","url":"https://www.compraonline.alcampo.es/products/kin-enjuague-bucal/894645","precio":1.25,"nota":"precio del lineal"}'

# Solo precio por medida (lo vendido al peso): "KG" y "kg" son lo mismo
curl -s -X PUT http://127.0.0.1:8080/api/precios-manuales \
  -H 'Content-Type: application/json' \
  --data '{"cadena":"alcampo","url":"https://www.compraonline.alcampo.es/products/platanos/2","precio":0,"precioMedida":1.75,"medida":"KG"}'

# Todos, con el nombre; o solo uno
curl -s http://127.0.0.1:8080/api/precios-manuales
curl -s --get http://127.0.0.1:8080/api/precios-manuales \
  --data-urlencode 'cadena=alcampo' --data-urlencode 'url=...'

# Quitarlo (el de la tienda, si lo había, se queda como estaba)
curl -s -X DELETE --get http://127.0.0.1:8080/api/precios-manuales \
  --data-urlencode 'cadena=alcampo' --data-urlencode 'url=...'
```

Un precio manual manda en el subtotal de su línea (`precioFuente: "manual"` en la
línea) y cuenta como recién escrito: el producto no sale como caducado aunque el
precio de la tienda sea viejo. Borrarlo no toca el de la tienda.

## Mi compra

`/api/mi-compra` es la lista de lo que vas a comprar, con lo que suma por tienda y
el total. La cantidad es un número real, así que 1,5 kg de plátanos es una cantidad
válida:

```sh
# Añadir un producto (sin "cantidad" cuenta como 1). Responde 201 con la lista entera
curl -s -X POST http://127.0.0.1:8080/api/mi-compra \
  -H 'Content-Type: application/json' \
  --data '{"cadena":"mercadona","url":"https://tienda.mercadona.es/product/3723/fresas-bandeja"}'

# Cambiar la cantidad; con 0 o menos lo quita
curl -s -X PUT http://127.0.0.1:8080/api/mi-compra \
  -H 'Content-Type: application/json' \
  --data '{"cadena":"mercadona","url":"https://tienda.mercadona.es/product/3723/fresas-bandeja","cantidad":2}'

# Verlo todo
curl -s http://127.0.0.1:8080/api/mi-compra

# Quitar un producto, o vaciar la lista entera (sin parámetros)
curl -s -X DELETE --get http://127.0.0.1:8080/api/mi-compra \
  --data-urlencode 'cadena=mercadona' --data-urlencode 'url=...'
curl -s -X DELETE http://127.0.0.1:8080/api/mi-compra
```

La respuesta trae `items` (una línea por producto), `subtotalPorCadena` (solo las
tiendas con algo que sumar, de mayor a menor), `total`, `sinPrecio`,
`sinPrecioDetalle` y `avisoTotal`. Con una leche de Mercadona a 1,10 € × 2, un pan
de Alcampo con precio manual a 1,25 € × 1 y plátanos que solo se publican por kilos
(1,65 €/kg):

```json
{
  "items": [
    {
      "id": 1,
      "cadena": "mercadona",
      "nombreCadena": "Mercadona",
      "url": "https://tienda.mercadona.es/product/1/leche",
      "nombre": "Leche entera",
      "formato": "1 L",
      "cantidad": 2,
      "precio": 1.1,
      "precioFuente": "web",
      "precioMedida": 1.1,
      "medida": "l",
      "precioSoloMedida": false,
      "subtotal": 2.2,
      "añadido": "2026-10-02T10:00:00Z"
    },
    {
      "id": 2,
      "cadena": "alcampo",
      "nombreCadena": "Alcampo",
      "url": "https://www.alcampo.es/products/pan/1",
      "nombre": "Pan de molde",
      "formato": "600 g",
      "cantidad": 1,
      "precio": 1.25,
      "precioFuente": "manual",
      "precioMedida": 0,
      "medida": "",
      "precioSoloMedida": false,
      "subtotal": 1.25,
      "añadido": "2026-10-02T10:01:00Z"
    },
    {
      "id": 3,
      "cadena": "alcampo",
      "nombreCadena": "Alcampo",
      "url": "https://www.alcampo.es/products/platanos/2",
      "nombre": "Plátanos",
      "formato": "",
      "cantidad": 1,
      "precio": 0,
      "precioFuente": "ninguno",
      "precioMedida": 1.65,
      "medida": "kg",
      "precioSoloMedida": true,
      "subtotal": 0,
      "añadido": "2026-10-02T10:02:00Z"
    }
  ],
  "subtotalPorCadena": [
    {"cadena": "mercadona", "nombre": "Mercadona", "subtotal": 2.2},
    {"cadena": "alcampo", "nombre": "Alcampo", "subtotal": 1.25}
  ],
  "total": 3.45,
  "sinPrecio": 1,
  "sinPrecioDetalle": ["Plátanos"],
  "avisoTotal": "El total no incluye 1 línea por no tener precio de unidad: o la tienda solo publica el precio por medida (€/kg o €/l), que no dice cuánto cuesta una bolsa, o todavía no hay precio para esa ficha. Están en sinPrecioDetalle, sin estimar nada."
}
```

**El total solo suma precios de unidad**, y esa es la parte que conviene entender
antes de fiarse de él. Lo vendido al peso (plátanos a 1,65 €/kg) no tiene precio de
unidad: su `precio` está a 0 **a propósito** y de un €/kg no se puede saber cuánto
cuesta una bolsa. Multiplicar el €/kg por una cantidad inventada daría un total
falso, y **un total falso es peor que un total con huecos**, así que esas líneas no
se estiman: se cuentan aparte.

`sinPrecio` cuenta **toda línea cuyo precio sea 0 o menor**, así que no es solo «lo
vendido al peso»: también entra lo que todavía no tiene precio, como una ficha de
Alcampo sin precio escrito o una que la cola todavía no ha descargado.
`sinPrecioDetalle` dice cuáles son, y `avisoTotal` lo explica en una frase (va vacío
cuando no hay ninguna). Si `sinPrecio` es 0, el total sí está completo.

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
`--cadenas`, `--log`. Además, `crawl index` acepta `--max N`, `crawl precios`
acepta `--limit N` y `smoke` acepta `--url cadena=url`.

Sin Docker hace falta Go 1.27 y un Chromium/Chrome en el `PATH` (si no,
`SUPERCOMPARATOR_BROWSER_BIN=/ruta/a/chrome`):

```sh
make serve                 # o: go run ./cmd/supercomparator serve
make index                 # o: go run ./cmd/supercomparator crawl index
go run ./cmd/supercomparator crawl precios --limit 20   # una pasada acotada
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
`name_source = categoria`. Cuando la cola visita la ficha escribe el nombre real
que publica (`store.FichaData` pone `name_source = 'ficha'` y saca el producto de
`ficha_pendiente`), así que sus productos dejan de llamarse «leche» y aparecen al
buscar «leche entera». Hasta que la cola llega, lo que se busca en DÍA son
categorías, no productos concretos.

**Alcampo.** Su catálogo se lee muy bien (89.615 productos), pero las fichas están
detrás de un WAF que responde 403 a los clientes automatizados, así que no sale
ningún precio. De ahí que venga con `precios_activos = 0`. **No se intenta
saltarse ese WAF.** Para eso están los precios que pones tú a mano: como sus fichas
están catalogadas pero sin precio, escribes lo que ves en el tienda y ya puedes
sumarlo en «Mi compra».

## Qué no existe todavía

Para no prometer nada que no esté en el repo:

- **La SPA de Angular.** Hoy `GET /` devuelve una página HTML mínima que lista la
  API. La API está lista; la web no. «Mi compra» y los precios a mano son
  endpoints y tablas: de interfaz no hay nada.
- **La comparación «en otras tiendas».** No hay ninguna vista que agrupe el mismo
  producto entre cadenas: la comparación la hace la persona, producto a producto,
  con `precioMedida` a la vista.
- Tampoco hay: recomprobación de un precio viejo al abrirlo (el SSE solo avisa
  del progreso de la cola, no de un precio caducado) ni consulta del histórico de
  cambios por la API (`store.Changes` existe en el código, pero ningún endpoint
  lo llama) ni reanudar el indexado desde `crawl_state` (`crawl index` relee el
  sitemap entero).

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