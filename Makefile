BIN := supercomparator
VERSION := 0.4.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -X github.com/miferdev/superComparator/internal/version.Version=$(VERSION) \
           -X github.com/miferdev/superComparator/internal/version.Commit=$(COMMIT)

.PHONY: build test test-integration vet fmt serve run index docker-build up clean

build:
	go build -ldflags="$(LDFLAGS)" -o bin/$(BIN) ./cmd/supercomparator

test:
	go test ./...

test-integration:
	go test -tags integration ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# serve es lo que hace el comando por defecto: levanta la web del catálogo.
serve:
	go run ./cmd/supercomparator serve

run: serve

# index lee los sitemaps y guarda el catálogo. Los precios los rellena la cola
# de precios, que todavía no está (fase 2).
index:
	go run ./cmd/supercomparator crawl index

docker-build:
	docker compose build

# up recompila siempre: sin --build se reutiliza la imagen anterior y los
# informes pueden salir con código viejo.
up:
	docker compose up --build

clean:
	rm -rf bin datos
