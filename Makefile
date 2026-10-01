BIN := supercomparator
VERSION := 0.3.0-dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -X github.com/miferdev/superComparator/internal/version.Version=$(VERSION) \
           -X github.com/miferdev/superComparator/internal/version.Commit=$(COMMIT)

.PHONY: build test test-integration vet fmt run check report explain docker-build up clean

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

run:
	go run ./cmd/supercomparator

check:
	go run ./cmd/supercomparator check

report:
	go run ./cmd/supercomparator report

explain:
	go run ./cmd/supercomparator explain

docker-build:
	docker compose build

# up recompila siempre: sin --build se reutiliza la imagen anterior y los
# informes pueden salir con código viejo.
up:
	docker compose up --build

clean:
	rm -rf bin datos
