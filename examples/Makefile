CONTAINER_NAME := tomei-examples
IMAGE_NAME := tomei-examples:latest

GOOS ?= linux
GOARCH ?= $(shell go env GOARCH)

.PHONY: build run clean help

build: ## Build tomei binary and example container
	$(MAKE) -C .. vendor-cue
	# CGO_ENABLED=0 produces a statically-linked binary so the container
	# (ubuntu:24.04, glibc) can execute the binary regardless of the
	# host's libc / dynamic linker. Without this, Nix / Alpine hosts
	# bake in an absolute interp path (e.g., /nix/store/.../ld-linux.so)
	# that the container cannot resolve, producing the famously cryptic
	# `bash: ./tomei: cannot execute: required file not found`.
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o tomei ../cmd/tomei
	docker build -t $(IMAGE_NAME) .

run: ## Run interactive shell in example container
	# Pass GITHUB_TOKEN / GH_TOKEN through if set on the host. tomei init's
	# aqua-registry fetch goes through api.github.com (to resolve the
	# latest ref) and raw.githubusercontent.com (to fetch registry content).
	# Anonymous traffic from a fresh container is rate-limited to the point
	# that fresh containers reliably hit HTTP 403 within seconds. Without a
	# token, the aqua-based tools in real-world/ (k8s, utility, privileged)
	# all fail with "aqua-registry resolver not configured".
	docker run --rm -it --name $(CONTAINER_NAME) \
		-e GITHUB_TOKEN="$${GITHUB_TOKEN:-}" \
		-e GH_TOKEN="$${GH_TOKEN:-}" \
		$(IMAGE_NAME)

clean: ## Clean up
	rm -f tomei
	-docker rmi $(IMAGE_NAME) 2>/dev/null || true

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
