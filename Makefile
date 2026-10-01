VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
CONTAINER ?= docker
DB_URL    ?= postgres://netprobe:netprobe@127.0.0.1:5432/netprobe?sslmode=disable
PKG     := github.com/Arylite/netprobe/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.DEFAULT_GOAL := help
.PHONY: help build test test-db dev-db lint fmt clean

help: ## List the targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

build: ## Build both binaries into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ ./cmd/...

test: ## Run the tests with the race detector (database tests are skipped)
	go test -race ./...

test-db: ## Run all the tests against the database of dev-db
	NETPROBE_TEST_DATABASE_URL="$(DB_URL)" go test -race -count=1 ./...

dev-db: ## Start a TimescaleDB for development and tests on 127.0.0.1:5432
	$(CONTAINER) run -d --name netprobe-db -p 127.0.0.1:5432:5432 -e POSTGRES_USER=netprobe -e POSTGRES_PASSWORD=netprobe -e POSTGRES_DB=netprobe timescale/timescaledb:2.30.1-pg18

lint: ## Run go vet and golangci-lint
	go vet ./...
	golangci-lint run ./...

fmt: ## Format the code
	golangci-lint fmt ./...

clean: ## Remove the build output
	rm -rf bin
