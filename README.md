# SuperComparator

Comparador de precios de la compra entre **Mercadona** y **Ahorramas** a partir
de una lista en markdown. Resuelve cada producto, compara por unidad y por peso
(€/kg, €/L) y detecta cambios de precio, ofertas y productos descatalogados.

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
docker compose run --rm app
```

La primera vez construye la imagen. Nota: `docker compose up` no adjunta la
entrada estándar (está pensado para logs de servicios), así que la TUI no
recibiría teclado; para apps interactivas el verbo de Compose es `run`. La TUI
abre un selector para elegir tu `lista.md` y estos atajos:

- `r` — resuelve los productos en ambas cadenas (muestra el progreso).
- `c` — comparativa: total por cadena, ganador por producto y compra mixta.
- `h` — historial de cambios de precio y descatalogados.
- `enter` — ver alternativas de un producto con confianza baja y elegir otra.
- `q` — salir.

El historial y el informe quedan en `./datos/` (`precios.db` e `informe.md`).
Para un re-chequeo sin interfaz (cron):

```sh
docker compose run --rm app check
```

## Uso local (sin Docker)

Requiere Go 1.27 y un Chromium/Chrome instalado. Si no está en el `PATH`:

```sh
SUPERCOMPARATOR_BROWSER_BIN=/ruta/a/chrome go run ./cmd/supercomparator
go run ./cmd/supercomparator check
```

## Configuración (variables de entorno)

| Variable                     | Por defecto            | Descripción                          |
| ---------------------------- | ---------------------- | ------------------------------------ |
| `SUPERCOMPARATOR_CP`         | `28032`                | Código postal para Mercadona         |
| `SUPERCOMPARATOR_LISTA`      | (vacío)                | Ruta de `lista.md` (omite el selector) |
| `SUPERCOMPARATOR_LISTA_DIR`  | `/compras` o `.`       | Carpeta que abre el selector         |
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
