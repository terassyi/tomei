CONTAINER_NAME := tomei-e2e-ubuntu
IMAGE_NAME := tomei-ubuntu:test

# Allow overriding GOARCH via environment variable (default: host architecture)
GOOS ?= linux
GOARCH ?= $(shell go env GOARCH)

.PHONY: build up down test clean help

build: ## Build tomei binary and Docker image
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o tomei ../cmd/tomei
	docker build -t $(IMAGE_NAME) -f containers/ubuntu/Dockerfile .

up: ## Start the test container
	docker run -d --name $(CONTAINER_NAME) $(IMAGE_NAME) sleep infinity

down: ## Stop and remove the test container
	-docker rm -f $(CONTAINER_NAME) 2>/dev/null || true

test: ## Run E2E tests with Ginkgo (container mode)
	TOMEI_E2E_CONTAINER=$(CONTAINER_NAME) go test -tags=e2e -v ./... -ginkgo.v

test-native: ## Run E2E tests natively (requires TOMEI_E2E_BINARY)
	TOMEI_E2E_NATIVE=true go test -tags=e2e -v ./... -ginkgo.v

exec: ## Exec into the test container
	docker exec -it $(CONTAINER_NAME) bash

clean: down ## Clean up build artifacts and Docker image
	rm -f tomei
	-docker rmi $(IMAGE_NAME) 2>/dev/null || true

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
