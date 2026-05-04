.DEFAULT_GOAL := help

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/MertDalbudak/mcsm/internal/buildinfo.Version=$(VERSION) \
	-X github.com/MertDalbudak/mcsm/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/MertDalbudak/mcsm/internal/buildinfo.Date=$(BUILD_DATE)

BIN := bin/mcsm

.PHONY: help build run test vet fmt tidy docker clean install

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build the mcsm + mcsm-tokens binaries into bin/
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/mcsm
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/mcsm-tokens ./cmd/mcsm-tokens

run: build ## Build then run with configs/config.example.yaml
	$(BIN) --config configs/config.example.yaml

test: ## Run tests
	go test ./...

vet: ## go vet
	go vet ./...

fmt: ## gofmt -w
	gofmt -w .

tidy: ## go mod tidy
	go mod tidy

docker: ## Build the docker image
	docker build \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg COMMIT=$(COMMIT) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  -t mcsm:$(VERSION) \
	  -t mcsm:latest \
	  -f deploy/docker/Dockerfile .

install: build ## Install the binary to /usr/local/bin (sudo required)
	install -m 0755 $(BIN) /usr/local/bin/mcsm

clean: ## Remove build artifacts
	rm -rf bin/ dist/
