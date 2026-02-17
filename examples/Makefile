CONTAINER_NAME := tomei-examples
IMAGE_NAME := tomei-examples:latest

GOOS ?= linux
GOARCH ?= $(shell go env GOARCH)

.PHONY: build run clean help

build: ## Build tomei binary and example container
	$(MAKE) -C .. vendor-cue
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o tomei ../cmd/tomei
	docker build -t $(IMAGE_NAME) .

run: ## Run interactive shell in example container
	docker run --rm -it --name $(CONTAINER_NAME) $(IMAGE_NAME)

clean: ## Clean up
	rm -f tomei
	-docker rmi $(IMAGE_NAME) 2>/dev/null || true

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
