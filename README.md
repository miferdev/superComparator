# SuperComparator

Comparador de precios de la compra entre **Mercadona**, **Ahorramas** y **DÍA** a
partir de una lista en markdown. Resuelve cada producto, compara por unidad y por
peso (€/kg, €/L) y escribe un informe markdown por supermercado y otro final con la
opción más barata de cada producto y su enlace directo.

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
repositorio, resuelve cada producto en las tres cadenas, comprueba los precios y
escribe los informes en `./datos/`. No hay interfaz interactiva: termina solo.

Los informes quedan en tu repositorio y son tuyos (el contenedor escribe con tu
usuario), así que puedes leerlos, editarlos o borrarlos sin `sudo`:

| Fichero                 | Contenido                                                             |
| ----------------------- | --------------------------------------------------------------------- |
| `datos/informe.md`      | Comparativa final: opción más barata, supermercado y enlace           |
| `datos/mercadona.md`    | Tu lista con el producto encontrado y el precio en Mercadona          |
| `datos/ahorramas.md`    | Ídem en Ahorramas                                                      |
| `datos/dia.md`          | Ídem en DÍA                                                            |
| `datos/alcampo.md`      | Ídem en Alcampo (solo si lo activas, ver abajo)                       |

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
| `SUPERCOMPARATOR_CADENAS`    | `mercadona,ahorramas,dia` | Cadenas a comparar                 |
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

Para comparar solo algunas cadenas:

```sh
docker compose run --rm app --cadenas mercadona,dia
```

## Los informes

**`datos/<cadena>.md`** copia tu lista tal cual y añade el producto encontrado,
su precio, el precio por kilo o litro, si está en oferta y el enlace a la ficha,
con el total de esa cadena al final.

**`datos/informe.md`** es la comparativa. Arriba dice **cómo comprar la lista
entera**: solo cuentan las cadenas que tienen **todos** los productos, así que una
cadena con un total parcial (porque le falte algo) nunca sale como la más barata.
Si ninguna tiene la lista completa, te lo dice y te propone la compra mixta. La
tabla de totales muestra cuántos productos cubre cada una (`3 de 3`, `1 de 3
_(parcial)_`) y en cuántas tiendas habría que entrar para la mixta.

Debajo, una fila por producto con el precio más barato, el supermercado, el
nombre del producto elegido y el enlace. Añade secciones solo si hacen falta:

- **Cambios de precio**: lo que ha variado desde las ejecuciones anteriores.
- **Sin comprar**: productos que no están en ninguna de las cadenas comparadas, que
  es lo que impide cerrar la lista del todo.
- **Revisar**: productos que ninguna cadena resolvió con confianza, con enlaces a
  los candidatos más parecidos para que decidas tú. Un producto no entra en los
  totales mientras sea dudoso, para no falsear el sumatorio.

En consola se imprime la misma comparativa en texto plano.

## Desarrollo

```sh
make test              # tests unitarios
make test-integration  # tests con red (opcional)
make build
```

El diseño y el orden de lectura del código están en [ARCHITECTURE.md](ARCHITECTURE.md).

## Las cadenas

| Cadena     | Cómo lee los datos                                   | Notas |
| ---------- | ---------------------------------------------------- | ----- |
| Mercadona  | sitemap por HTTP, fichas con navegador headless       | Fija el CP 28032 |
| Ahorramas  | sitemap y fichas por HTTP (JSON-LD)                   | |
| DÍA        | sitemap y fichas por HTTP (JSON-LD)                   | Ver limitaciones |
| Alcampo    | sitemaps por HTTP, fichas con navegador headless      | Desactivada por defecto |

**DÍA.** Su sitemap solo trae la ruta de la categoría
(`/huevos-leche-y-mantequilla/leche/p/16065`), sin el nombre del producto. Para
compensarlo, el adaptador elige las categorías que mejor encajan con tu línea,
muestrea unas pocas fichas de cada una y compara con el nombre real que publica
la ficha. Es una búsqueda por muestreo: puede no encontrar productos que sí tiene
(categorías sin cobertura) y todo lo que no encaje con confianza va a la sección
**Revisar** en vez de darse por bueno.

**Alcampo.** Su catálogo se lee bien (100 000 productos por sitemap), pero las
fichas están detrás de un WAF que responde 403 a los clientes automatizados, así
que no se obtienen precios. Por eso **no se compara por defecto**; si algún día la
web lo permite, se activa con `--cadenas mercadona,ahorramas,dia,alcampo`.

## Scraping responsable

Solo se acceden a rutas permitidas por el `robots.txt` de cada cadena:
`sitemaps` y fichas de producto. Nunca a sus endpoints de búsqueda o API interna.
El `User-Agent` identifica al proyecto de forma honesta
(`supercomparator/0.1 (+https://github.com/miferdev/superComparator)`) en las
peticiones HTTP. Concurrencia máxima 3 y pausas entre peticiones. Proyecto
personal y educativo: las webs pueden cambiar y el scraper deberá adaptarse.

## Licencia

MIT.
