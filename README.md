# SuperComparator

Comparador de precios entre **Mercadona** y **Ahorramas** a partir de tu lista
de la compra en markdown. Resuelve cada producto, compara por unidad y por peso
(€/kg, €/L) y detecta cambios de precio, ofertas y productos descatalogados.

> Estado: **F0 (scaffold)**. La funcionalidad llega en las siguientes fases.

## Tu lista (`lista.md`)

Una tabla markdown de dos columnas: producto y cantidad. Si la cantidad está
vacía, se asume **1 unidad**.

```md
| Producto        | Cantidad |
| --------------- | -------- |
| Leche entera 1L | 3        |
| Pan de molde    |          |
| Champú          | 1        |
```

El fichero `lista.md` se ignora en git (es tuya y personal); tienes un ejemplo
en `lista.ejemplo.md`.

## Uso previsto

```sh
docker compose up
```

La TUI abre un selector para elegir tu `lista.md`, resuelve los productos y
muestra la comparativa. El historial y el informe quedan en `./datos/`.

## Stack

- **Go 1.27** con **Bubble Tea v2** para la TUI (en español).
- **rod** (headless Chromium) para Mercadona; **HTTP + JSON-LD** para Ahorramas.
- **SQLite** (modernc, sin cgo) para historial de precios.
- Docker con `chromedp/headless-shell`.

## Desarrollo

```sh
go build ./...
go test ./...
go vet ./...
```

## Licencia

MIT. Uso personal y educativo: el scraper solo accede a rutas permitidas por
el `robots.txt` de cada cadena (sitemaps y fichas de producto).
