FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/supercomparator ./cmd/supercomparator

FROM chromedp/headless-shell:stable
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/supercomparator /usr/local/bin/supercomparator

ENV ROD_BROWSER_BIN=/headless-shell/headless-shell \
    SUPERCOMPARATOR_CP=28032 \
    SUPERCOMPARATOR_LISTA_DIR=/compras \
    SUPERCOMPARATOR_DB=/datos/precios.db \
    SUPERCOMPARATOR_REPORT=/datos/informe.md

WORKDIR /compras
ENTRYPOINT ["supercomparator"]
