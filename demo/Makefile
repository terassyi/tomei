CONTAINER := tomei-demo
IMAGE := tomei-demo:latest

.PHONY: record clean help

record: ## Record demo GIF (requires vhs: go install github.com/charmbracelet/vhs@latest)
	docker build -t $(IMAGE) .
	docker rm -f $(CONTAINER) 2>/dev/null || true
	docker run -d --name $(CONTAINER) $(IMAGE) sleep infinity
	vhs demo.tape
	docker rm -f $(CONTAINER)

clean: ## Remove generated files
	rm -f demo.gif

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
