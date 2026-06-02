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
	# Pass GITHUB_TOKEN / GH_TOKEN through if set on the host. tomei init
	# resolves the latest aqua-registry ref via api.github.com; the
	# actual registry content is fetched later from raw.githubusercontent.com
	# when aqua-based tools are installed. Both endpoints are rate-limited
	# for anonymous traffic, so fresh containers reliably hit HTTP 403
	# within seconds. Without a token, the aqua-based tools in real-world/
	# (k8s, utility, privileged) all fail with
	# "aqua-registry resolver not configured".
	# `-e VAR` (no =value) forwards the host env var without putting its
	# value on the docker argv, so the token can't leak via `ps`/shell
	# history. If neither var is set on the host, both are simply absent
	# inside the container — which is the same as the previous behavior.
	docker run --rm -it --name $(CONTAINER_NAME) \
		-e GITHUB_TOKEN \
		-e GH_TOKEN \
		$(IMAGE_NAME)

clean: ## Clean up
	rm -f tomei
	-docker rmi $(IMAGE_NAME) 2>/dev/null || true

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
