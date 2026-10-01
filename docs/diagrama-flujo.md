# Diagrama de flujo

Cómo funciona el software de punta a punta: qué pasa al arrancar, qué hace el
indexado de catálogos, qué debería hacer la cola de precios y qué ve la persona
que usa la web. **El diagrama es el diseño completo; al final se indica qué parte
está construida hoy**, porque la cola de precios y la SPA todavía no existen.

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
   Retoma los trabajos a medias (crawl_state y price_queue)
            |
            v
   Sirve la API en 127.0.0.1:8080
            |
            +----> lanza el indexado de catalogos      (caja 1)
            |
            +----> lanza la cola de precios            (caja 2)
            |
            +----> espera las peticiones del usuario   (caja 3)


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

   +--> Coge un producto pendiente de price_queue
   |          |
   |          v
   |     Espera el ritmo que marque esa cadena
   |          |
   |          v
   |     Descarga la ficha
   |          |
   |          v
   |     +- - - - - - - - - - - - - - - - - - - - - - +
   |     |  ¿La web responde?                             |
   |     |                                                |
   |     |  NO (403 o WAF)          SI                    |
   |     |     |                     |                     |
   |     |     v                     v                     |
   |     |  Guarda el error      Extrae precio, si es      |
   |     |  y suma un intento    por kg, y la medida      |
   |     |     |                     |                     |
   |     |     |                     v                     |
   |     |     |              products + price_history    |
   |     |     |                     |                     |
   |     |     |                     v                     |
   |     |     |              Marca hecho                |
   |     |     |                     |                     |
   |     +----------+----------+                     |
   |     |                |                                |
   |     |                v                                |
   |     |        +-----------------------+                 |
   |     +------->| ¿Muchos fallos seguidos?|<-----------+
   |              +-----------------------+
   |                 si |          | no
   |                    v          v
   |              Pausa esa     Dormir un rato
   |              cadena        y volver a
   |              (las demas    empezar
   |              siguen)
   |                    |
   +--------------------+
        (vuelve a por el siguiente producto)


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
   |  guardado                   (y el SSE te avisa              |
   |     |                        cuando este listo)              |
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
- La cola de precios **no** se lanza: su worker no existe todavía (caja 2).
- `crawl_state` y `price_queue` ya guardan el punto de partida, pero el indexador
  todavía no los reanuda: `crawl index` relee el sitemap entero y, como el
  `upsert` es idempotente sobre `(chain, url)`, no duplica nada ni pisa precios.

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

Es la parte que más tarda y la que todavía no está escrita. Lo que **sí** existe
es todo el estado y las operaciones de `store`: `EnqueuePrices`, `NextQueued`
(saca entradas y las marca `descargando` en la misma transacción, para que dos
workers no peleen por la misma ficha), `FailQueue` (intentos y
`next_attempt_at` para el backoff) y `MarkQueueDone`. Falta el worker que las
consuma: el que espera el ritmo de la cadena, llama a `chain.Chain.Fetch`, guarda
con `store.SetPrice` (producto + `price_history` en la misma transacción), pausa
una cadena cuando encadena fallos y avisa al navegador. `precios_activos` está a
0 en Alcampo porque su WAF responde 403 a las fichas.

Cuando exista, la cola **se reanuda sola desde SQLite**: como la cola y los
intentos están en tablas y no en memoria, cerrar el contenedor no pierde nada.

### Caja 3 — Lo que haces en la web

Hoy la web es una página HTML mínima que lista la API, y lo que funciona de todo
el recorrido es la primera mitad: buscar y filtrar (`GET /api/catalogo`, con FTS5
y paginación), ver un producto (`GET /api/producto`) y ver el estado de cada
cadena (`GET /api/cadenas`, `GET /api/estado`). La API ya marca los precios viejos
(`precioViejo` + `frescuraHoras`, contra `precio_max_horas` de la cadena), que es
la señal que usará la SPA para pedir una comprobación al momento.

Lo que **no** existe todavía: la SPA de Angular, la recomprobación en vivo de un
precio viejo y su aviso por SSE, la vista «el mismo producto en otras tiendas», la
selección entre cadenas y «Mi lista» con subtotales (la tabla `list_items` está
creada y vacía, sin endpoints).