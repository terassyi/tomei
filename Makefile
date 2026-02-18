.PHONY: build test test-integration test-e2e test-all test-cover lint fmt clean help docker-build docker-run module-publish vendor-cue unvendor-cue

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

EXAMPLE_DIRS := examples/minimal examples/real-world

vendor-cue: ## Vendor CUE schema and presets into examples/*/cue.mod/pkg/
	@for dir in $(EXAMPLE_DIRS); do \
		rm -rf $$dir/cue.mod/pkg/tomei.terassyi.net; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/schema; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/go; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/rust; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/aqua; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/node; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/python; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/deno; \
		mkdir -p $$dir/cue.mod/pkg/tomei.terassyi.net/presets/bun; \
		cp cuemodule/schema/schema.cue $$dir/cue.mod/pkg/tomei.terassyi.net/schema/; \
		cp cuemodule/presets/go/go.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/go/; \
		cp cuemodule/presets/rust/rust.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/rust/; \
		cp cuemodule/presets/aqua/aqua.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/aqua/; \
		cp cuemodule/presets/node/node.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/node/; \
		cp cuemodule/presets/python/python.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/python/; \
		cp cuemodule/presets/deno/deno.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/deno/; \
		cp cuemodule/presets/bun/bun.cue $$dir/cue.mod/pkg/tomei.terassyi.net/presets/bun/; \
		sed -i.bak '/^deps:/,/^}/d' $$dir/cue.mod/module.cue && rm -f $$dir/cue.mod/module.cue.bak; \
	done

unvendor-cue: ## Remove vendored CUE files and restore module.cue
	@for dir in $(EXAMPLE_DIRS); do \
		rm -rf $$dir/cue.mod/pkg; \
	done
	git checkout -- $(addsuffix /cue.mod/module.cue,$(EXAMPLE_DIRS))

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

docker-build: ## Build the development container
	docker build -t $(IMAGE_NAME) .

docker-run: ## Run interactive shell in the container
	docker run --rm -it -v $(PWD):/workspace $(IMAGE_NAME) bash

.DEFAULT_GOAL := help
