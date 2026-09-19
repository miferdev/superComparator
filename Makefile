BIN := supercomparator

.PHONY: build test test-integration vet fmt run check report docker-build up clean

build:
	go build -o bin/$(BIN) ./cmd/supercomparator

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

docker-build:
	docker compose build

up:
	docker compose run --rm app

clean:
	rm -rf bin datos
