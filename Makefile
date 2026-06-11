SERVICE := session-manager
IMAGE ?= dendenmushi/$(SERVICE):local
PORT ?= 8081
STORE_DRIVER ?= memory
DATABASE_URL ?=
GOCACHE ?= /private/tmp/ddm-go-build-cache
GOMODCACHE ?= /private/tmp/ddm-go-mod-cache

.PHONY: run run-postgres test test-postgres build docker-build clean

run:
	PORT=$(PORT) STORE_DRIVER=$(STORE_DRIVER) DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

run-postgres:
	docker compose up -d --wait postgres
	PORT=$(PORT) STORE_DRIVER=postgres DATABASE_URL=postgres://dendenmushi:dendenmushi@localhost:5432/dendenmushi?sslmode=disable go run ./cmd/server

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

test-postgres:
	docker compose up -d --wait postgres
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) TEST_DATABASE_URL=postgres://dendenmushi:dendenmushi@localhost:5432/dendenmushi?sslmode=disable go test ./...

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o bin/$(SERVICE) ./cmd/server

docker-build:
	docker build -t $(IMAGE) .

clean:
	rm -rf bin
