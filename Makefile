SERVICE := session-manager
IMAGE ?= dendenmushi/$(SERVICE):local
PORT ?= 8081
STORE_DRIVER ?= memory
DATABASE_URL ?=
GOCACHE ?= /private/tmp/ddm-go-build-cache
GOMODCACHE ?= /private/tmp/ddm-go-mod-cache

.PHONY: run test build docker-build clean

run:
	PORT=$(PORT) STORE_DRIVER=$(STORE_DRIVER) DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o bin/$(SERVICE) ./cmd/server

docker-build:
	docker build -t $(IMAGE) .

clean:
	rm -rf bin
