.PHONY: build test test-integration test-e2e test-all test-cover lint fmt clean help docker-build docker-run module-publish

BINARY_NAME := tomei
BUILD_DIR := bin
IMAGE_NAME := tomei-dev

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

build: ## Build the binary
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tomei

test: ## Run unit tests
	go test -v -race ./internal/... ./cmd/...

test-integration: ## Run integration tests (requires network access, Linux amd64 only)
	go test -v -race -tags=integration ./tests/...

test-e2e: ## Run E2E tests (requires Docker)
	$(MAKE) -C e2e build
	$(MAKE) -C e2e up
	$(MAKE) -C e2e test; ret=$$?; $(MAKE) -C e2e down; exit $$ret

test-all: test test-integration test-e2e ## Run all tests

test-cover: ## Run tests with coverage
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linter
	golangci-lint run ./...

fmt: ## Format code
	golangci-lint fmt ./...

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -f tomei
	$(MAKE) -C e2e clean

MODULE_VERSION ?= $(VERSION)

module-publish: ## Publish CUE module to OCI registry via cue mod publish
	cd cuemodule && cue mod publish $(MODULE_VERSION)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

docker-build: ## Build the development container
	docker build -t $(IMAGE_NAME) .

docker-run: ## Run interactive shell in the container
	docker run --rm -it -v $(PWD):/workspace $(IMAGE_NAME) bash

.DEFAULT_GOAL := help
