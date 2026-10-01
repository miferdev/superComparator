# SuperComparator

Comparador de precios de la compra entre **Mercadona** y **Ahorramas** a partir
de una lista en markdown. Resuelve cada producto, compara por unidad y por peso
(€/kg, €/L) y escribe un informe markdown con la opción más barata de cada
supermercado y un enlace directo al producto.

## Tu lista (`lista.md`)

Una tabla markdown de dos columnas: producto y cantidad. Si la cantidad está
vacía o no tiene número, se asume **1 unidad**.

```md
| Producto         | Cantidad |
| ---------------- | -------- |
| Leche entera 1L  | 3        |
| Pan de molde     |          |
| Champú           | 1        |
```

Deja `lista.md` en la raíz del repositorio (junto a `compose.yml`). Tienes un
ejemplo en `lista.ejemplo.md`. El fichero personal se ignora en git.

## Uso con Docker

```sh
docker compose up
```

La primera vez construye la imagen. El programa lee `lista.md` de la raíz del
repositorio, resuelve cada producto en Mercadona y Ahorramas, comprueba los
precios y escribe el informe markdown en `./datos/informe.md`. No hay interfaz
interactiva: termina solo.

Para otra lista:

```sh
docker compose run --rm app --lista /compras/otra-lista.md
```

El historial de precios queda en `./datos/precios.db`, así que las siguientes
ejecuciones solo vuelven a consultar los productos ya resueltos. Eso también
sirve para cron:

```sh
docker compose run --rm app
```

## Uso local (sin Docker)

Requiere Go 1.27 y un Chromium/Chrome instalado. Si no está en el `PATH`:

```sh
SUPERCOMPARATOR_BROWSER_BIN=/ruta/a/chrome go run ./cmd/supercomparator
go run ./cmd/supercomparator --lista otra-lista.md
```

## Configuración (variables de entorno)

| Variable                     | Por defecto            | Descripción                          |
| ---------------------------- | ---------------------- | ------------------------------------ |
| `SUPERCOMPARATOR_CP`         | `28032`                | Código postal para Mercadona         |
| `SUPERCOMPARATOR_LISTA`      | `lista.md`             | Ruta de la lista de la compra        |
| `SUPERCOMPARATOR_DB`         | `datos/precios.db`     | Base SQLite                          |
| `SUPERCOMPARATOR_REPORT`     | `datos/informe.md`     | Informe markdown                     |
| `SUPERCOMPARATOR_WORKERS`    | `3`                    | Peticiones en paralelo               |
| `SUPERCOMPARATOR_DELAY_MS`   | `300`                  | Pausa entre peticiones               |
| `SUPERCOMPARATOR_CANDIDATES` | `6`                    | Candidatos que se descargan por cadena |
| `SUPERCOMPARATOR_RANK_POOL`  | `15`                   | Candidatos puntuados en el sitemap   |
| `SUPERCOMPARATOR_MIN_SCORE`  | `0`                    | Puntuación mínima para descargar un candidato |
| `SUPERCOMPARATOR_MAX_PRICE_AGE_DAYS` | `0`            | Caducidad del precio en días (0 = sin límite) |
| `SUPERCOMPARATOR_LOG`        | (vacío)                | Fichero de log (p. ej. `datos/app.log`); registra cada match con su similitud |
| `SUPERCOMPARATOR_BROWSER_BIN`| (auto)                 | Binario de Chromium                  |

## El informe

Cada ejecución escribe `./datos/informe.md` con una tabla por producto: precio
de la opción más barata, supermercado, nombre del producto elegido y enlace
directo a la ficha, más los totales por cadena, la compra mixta y el ahorro
máximo. En consola se imprime la misma comparativa.

## Desarrollo

```sh
make test              # tests unitarios
make test-integration  # tests con red (opcional)
make build
```

El diseño y el orden de lectura del código están en [ARCHITECTURE.md](ARCHITECTURE.md).

## Scraping responsable

Solo se acceden a rutas permitidas por el `robots.txt` de cada cadena:
`sitemap.xml` y fichas de producto. Nunca a sus endpoints de búsqueda o API
interna. Concurrencia máxima 3 y pausas entre peticiones. Proyecto personal y
educativo: las webs pueden cambiar y el scraper deberá adaptarse.

## Licencia

MIT.
