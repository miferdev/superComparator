FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# La versión y el commit se inyectan aquí para que los informes digan con qué
# código se generaron.
ARG VERSION=0.3.0-dev
ARG COMMIT=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/miferdev/superComparator/internal/version.Version=${VERSION} -X github.com/miferdev/superComparator/internal/version.Commit=${COMMIT}" \
    -o /out/supercomparator ./cmd/supercomparator

FROM chromedp/headless-shell:stable
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/supercomparator /usr/local/bin/supercomparator

ENV ROD_BROWSER_BIN=/headless-shell/headless-shell \
    SUPERCOMPARATOR_ADDR=0.0.0.0:8080 \
    SUPERCOMPARATOR_CP=28032 \
    SUPERCOMPARATOR_DB=/datos/catalogo.db

WORKDIR /datos
ENTRYPOINT ["supercomparator"]
