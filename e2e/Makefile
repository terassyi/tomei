CONTAINER_NAME := toto-e2e-ubuntu
IMAGE_NAME := toto-ubuntu:test

.PHONY: build up down test clean help

build: ## Build toto binary and Docker image
	GOOS=linux GOARCH=amd64 go build -o toto ../cmd/toto
	docker build -t $(IMAGE_NAME) -f containers/ubuntu/Dockerfile .

up: ## Start the test container
	docker run -d --name $(CONTAINER_NAME) $(IMAGE_NAME) sleep infinity

down: ## Stop and remove the test container
	-docker rm -f $(CONTAINER_NAME) 2>/dev/null || true

test: ## Run E2E tests with Ginkgo
	TOTO_E2E_CONTAINER=$(CONTAINER_NAME) go test -v ./... -ginkgo.v

exec: ## Exec into the test container
	docker exec -it $(CONTAINER_NAME) bash

clean: down ## Clean up build artifacts and Docker image
	rm -f toto
	-docker rmi $(IMAGE_NAME) 2>/dev/null || true

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
