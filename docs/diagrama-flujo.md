# Diagrama de flujo

Cómo funciona el software de punta a punta: qué pasa al arrancar, qué hace el
indexado de catálogos, cómo consume la cola de precios y qué ve la persona que
usa la web. **El diagrama es el diseño completo; al final se indica qué parte
está construida hoy**, porque la SPA todavía no existe.

```
+=====================================================================+
| ARRANQUE                                                            |
+====================================================================+

   Arranca el contenedor
            |
            v
   Abre SQLite y aplica migraciones
            |
            v
   Siembra la configuracion de las 4 cadenas
            |
            v
   Rescata las fichas de precio que quedaron a medias
   (ReclaimStale: lo que lleva mas de un minuto en 'descargando')
            |
            v
   Sirve la API en 127.0.0.1:8080
            |
            +----> lanza la cola de precios en segundo plano (caja 2)
            |
            +----> espera las peticiones del usuario   (caja 3)

   El indexado de catalogos (caja 1) no sale de aqui:
   es el comando `crawl index`.


+=====================================================================+
| CAJA 1 - INDEXAR CATALOGOS   (tarda segundos: son sitemaps)         |
+====================================================================+

   Lee el sitemap de la cadena
            |
            v
   Extrae nombre, medida, categoria y enlace
            |
            v
   Guarda en products, sin duplicar nada
            |
            v
   Encola en price_queue lo que aun no tiene precio
            |
            v
   Anota el avance en crawl_state y en runs
            |
            +----> pasa productos a la cola de precios   (caja 2)


+=====================================================================+
| CAJA 2 - COLA DE PRECIOS   (segundo plano: dura horas)             |
+====================================================================+

   Solo trabaja las cadenas con precios activos y para las que
   hay adaptador (Alcampo se queda fuera: su WAF responde 403)
             |
             v
   +--> Ronda por cadenas: reparte el trabajo por turnos
   |    para que el catalogo no se llene de una sola tienda
   |          |
   |          v
   |     NextQueuedChain reserva hasta N fichas de esa cadena
   |     y las marca 'descargando' EN LA MISMA TRANSACCION
   |     (asi dos workers nunca pelean por la misma ficha)
   |          |
   |          v
   |     Cada flujo de la cadena duerme su pausa_segundos
   |     (o la espera que le toque por reintento)
   |          |
   |          v
   |     Descarga la ficha (PriceFetcher -> adaptador de esa tienda)
   |          |
   |          v
   |     ¿La ficha trae un precio publicado?
   |          |
   |          +------ SI ------> catalog escribe el nombre y la categoria
   |          |                 de la ficha (FichaData), luego SetPrice
   |          |                 actualiza products y price_history en una
   |          |                 sola transaccion, y MarkQueueDone borra la
   |          |                 fila: el precio ya esta en products
   |          |
   |          +------ NO ------> la ficha se cuenta como fallida:
   |                            403 o WAF, ya no esta disponible, no trae
   |                            precio, o la ficha no existe (404)
   |          |
   |          v
   |     FailQueue: suma un intento y anota el error; la siguiente
   |     espera es el backoff (30 s doblando, tope de 5 min), asi que
   |     la pasada sigue y las demas cadenas siguen con su turno
   |          |
   |          v
   |     ¿Se agotaron los intentos (MaxIntentos = 5) o es un 404?
   |     Ninguno de los dos se arregla reintentando, asi que la ficha
   |     queda en estado 'error' y espera a que alguien la rearme
   |     (Requeue)
   |          |
   +----------+  (vuelve a por la siguiente ficha: un fallo no para
               el bucle; lo que aparta una tienda entera es el
               interruptor precios_activos, no un fallo)


+=====================================================================+
| CAJA 3 - LO QUE HACES EN LA WEB                                     |
+======================================================================+

   Abres http://127.0.0.1:8080
            |
            v
   Buscas o filtras por cadena, categoria o disponibilidad
            |
            v
   FTS5 devuelve paginas de productos con precio, medida y enlace
            |
            v
   GET /api/eventos: un evento 'estado' cada 2 s dice, por cadena,
   quantas fichas quedan pendientes, descargando o en error, y
   cuantas ya tienen precio (no hay que preguntar: es un stream)
            |
            v
   Abres un producto
            |
            v
   +- - - - - - - - - - - - - - - - - - - - - - - - - - - - - +
   |  ¿Su precio tiene mas de 24 horas?                          |
   |                                                              |
   |  NO                          SI                             |
   |     |                         |                              |
   |     v                         v                              |
   |  Muestra el precio        Se comprueba ahora mismo           |
   |  guardado                   (el SSE avisa del progreso      |
   |     |                        de la cola, no de esto)          |
   |     +-------------+---------------+                          |
   |                   v                                          |
   |          Muestra precio, medida y enlace                      |
   |                   |                                          |
   |                   v                                          |
   |          Ves el mismo producto en otras tiendas               |
   |                   |                                          |
   |                   v                                          |
   |              Comparas y eliges                               |
   |                   |                                          |
   |                   v                                          |
   |          Lo anades a "Mi lista"                               |
   |                   |                                          |
   |                   v                                          |
   |          Subtotal por tienda y total                          |
   +--------------------------------------------------------------+
```

## Las tres cajas

### Arranque

`cmd/supercomparator` (o `serve`) abre `datos/catalogo.db` con
`modernc.org/sqlite` en modo WAL, aplica las migraciones versionadas
(`internal/store/migrations.go`) y siembra la configuración de las cuatro cadenas
(`store.SeedChains`, que solo rellena lo vacío: si alguien cambia a mano el ritmo
o activa los precios de Alcampo, un arranque no lo deshace). Después levanta el
servidor HTTP en `127.0.0.1:8080` y a partir de ahí solo lee y escribe en la
base: **la web no habla nunca con una tienda**.

Lo que hay que tener claro hoy:

- El indexado de catálogos **no** se lanza solo: es el comando `crawl index`.
- La cola de precios **sí** se lanza sola, pero **en segundo plano**: `serve`
  arma el trabajo con `catalog.NewPricesJob` y lo echa a correr en una goroutine,
  así que la web levanta y responde aunque la cola tarde horas. Para probarla sin
  esperar horas está `crawl precios --limit N`, que hace **una sola pasada** y
  dice cuántas fichas salieron con precio y cuántas fallaron.
- `price_queue` guarda el punto de partida y **se reanuda sola**: si el proceso
  muere, `ReclaimStale` devuelve a `pendiente` lo que quedó en `descargando`
  (más de un minuto) y el trabajo sigue donde estaba. El indexador, en cambio,
  todavía no reanuda desde `crawl_state`: `crawl index` relee el sitemap entero y,
  como el `upsert` es idempotente sobre `(chain, url)`, no duplica nada ni pisa
  precios.

### Caja 1 — Indexar catálogos (rápido)

`catalog.Indexer` va cadena por cadena (una que falle no impide que las demás
terminen: el error se anota en su `crawl_state` y se sigue). De cada
`chain.SitemapEntry` saca nombre, medida, categoría y enlace y los mete en
`products` a lotes de 500 en una transacción (`store.UpsertProducts`). Después
mete en `price_queue` todo lo que aún no tiene precio (`store.EnqueuePrices`) y
cierra dejando el parte en `crawl_state` y en `runs`.

En la práctica, las cuatro cadenas juntas tardan alrededor de un minuto: los
106.226 productos del catálogo actual se indexaron en unos 40 s. `--max N` acota
cada cadena para probar.

De dónde sale cada campo es responsabilidad del adaptador de la cadena, y no es
igual en las cuatro: Alcampo y Ahorramas publican el nombre en el slug, Mercadona
también pero **sin la medida** (hay que visitarla), y DÍA **solo da la categoría**
—por eso sus filas nacen con `crawl_state = ficha_pendiente` y `name_source =
categoria`, a la espera de que la cola de precios les ponga el nombre real de la
ficha.

### Caja 2 — Cola de precios (segundo plano)

Está escrita: es `store.PriceLoop` (`internal/store/priceloop.go`), el consumidor
de `price_queue`. `Run(ctx, limit)` hace **una pasada** —hasta `limit` fichas por
cadena— y `RunUntilEmpty(ctx)` las repite hasta que no queda nada pendiente ni
reintentable; `serve` usa el segundo en segundo plano y `crawl precios` el
primero.

Una pasada, en orden:

1. **Rescate**, una sola vez por proceso: `ReclaimStale(ttl)` devuelve a
   `pendiente` lo que quedó en `descargando` hace más de un minuto (el proceso
   anterior murió con ellas). **No toca los intentos**: es trabajo perdido, no un
   fallo de la tienda, y sumarlos dejaría fichas buenas en `error`.
2. **Reparto por turnos** entre las cadenas (`NextQueuedChain`): cada ronda pide
   hasta `concurrencia` fichas a cada tienda activa, para que el catálogo no se
   llene de una sola mientras las demás esperan. La reserva marca `descargando` en
   la misma transacción, así que dos workers nunca pelean por la misma ficha.
3. **Ritmo**, por flujo y por cadena: cada flujo duerme su `pausa_segundos`, que
   sale de la tabla `chains` (`priceritmo.go`). Lo que aparta una tienda entera
   es el interruptor `precios_activos`, no un fallo.
4. **Descarga** con `PriceFetcher.Fetch(ctx, chainID, url)`, que es el adaptador
   de esa tienda. El bucle no conoce ninguna tienda: la interfaz la implementa
   `catalog`, que es quien tiene los adaptadores.
5. **Si hay precio publicado**: `catalog` escribe el nombre y la categoría
   definitivos de la ficha (`store.FichaData` — de aquí sale el nombre real de
   los productos de DÍA, cuyo sitemap solo traía la categoría), `store.SetPrice`
   actualiza el producto e inserta la muestra en `price_history` en la misma
   transacción, y `MarkQueueDone` borra la fila de la cola.
6. **Si no hay precio publicado** (la tienda devuelve 403, el producto ya no
   está, o la ficha no trae precio): `FailQueue` sube `attempts` y programa el
   siguiente intento con el backoff (30 s doblando hasta un tope de 5 min). Un
   fallo normal no para la pasada. A los `MaxIntentos` (5) la ficha queda en
   `error` y espera a que alguien la rearme con `Requeue`; igual se aparca un 404
   (`chain.ErrNotFound`), que reintentar no arregla.

La cola **se reanuda sola desde SQLite**: como la cola y los intentos están en
tablas y no en memoria, cerrar el contenedor no pierde nada. `precios_activos`
está a 0 en Alcampo porque su WAF responde 403 a las fichas, así que sus fichas
se quedan en la cola, que es justo donde deben estar.

### Caja 3 — Lo que haces en la web

Hoy la web es una página HTML mínima que lista la API, y lo que funciona de todo
el recorrido es la primera mitad: buscar y filtrar (`GET /api/catalogo`, con FTS5
y paginación), ver un producto (`GET /api/producto`), ver el estado de cada cadena
(`GET /api/cadenas`, `GET /api/estado`) y **seguir el progreso de la cola en vivo**
con `GET /api/eventos`: es un stream de Server-Sent Events que manda un evento
`estado` cada 2 segundos con, por cadena, `pendiente`, `descargando`, `error` y
`precios`. Se lee de la base, así que lo que ve la web es lo que de verdad se ha
descargado.

La API ya marca los precios viejos (`precioViejo` + `frescuraHoras`, contra
`precio_max_horas` de la cadena), que es la señal que usará la SPA para pedir una
comprobación al momento.

Lo que **no** existe todavía: la SPA de Angular, la recomprobación en vivo de un
precio viejo, la vista «el mismo producto en otras tiendas», la selección entre
cadenas y «Mi lista» con subtotales (la tabla `list_items` está creada y vacía,
sin endpoints).