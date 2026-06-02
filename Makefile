.DEFAULT_GOAL := help

GO        ?= go
PKG       := ./...
BIN_DIR   := bin
API_BIN   := $(BIN_DIR)/pgfleet-api

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	$(GO) mod tidy

.PHONY: build
build: ## Build the API server
	$(GO) build -o $(API_BIN) ./cmd/pgfleet-api

.PHONY: test
test: ## Run fast unit tests (no Docker)
	$(GO) test -race $(PKG)

.PHONY: test-integration
test-integration: ## Run integration tests (Docker required)
	$(GO) test -race -tags=integration -timeout 20m $(PKG)

.PHONY: cover
cover: ## Run unit tests with coverage report
	$(GO) test -race -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: fmt
fmt: ## Format code
	$(GO) fmt $(PKG)

.PHONY: vet
vet: ## Run go vet
	$(GO) vet $(PKG)

.PHONY: image
image: ## Build the managed postgres+pgBackRest image
	docker build -t pgfleet/postgres-pgbackrest:16 docker/postgres-pgbackrest

.PHONY: dev-up
dev-up: ## Start dev dependencies (meta-DB + MinIO)
	docker compose -f deploy/docker-compose.yml up -d

.PHONY: dev-down
dev-down: ## Stop dev dependencies
	docker compose -f deploy/docker-compose.yml down

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
