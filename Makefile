.DEFAULT_GOAL := help

GO        ?= go
PKG       := ./...
BIN_DIR   := bin
API_BIN   := $(BIN_DIR)/pgfleet-api

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: certs
certs: ## Generate self-signed TLS cert for the bundled MinIO (run once before `make run`)
	openssl req -x509 -newkey rsa:4096 -days 3650 -nodes \
		-keyout deploy/certs/private.key \
		-out    deploy/certs/public.crt \
		-subj   "/CN=pgfleet-minio" \
		-addext "subjectAltName=DNS:pgfleet-minio,DNS:localhost,IP:127.0.0.1"
	@echo "Certs written to deploy/certs/ — restart docker compose to pick them up"

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	$(GO) mod tidy

.PHONY: build
build: ## Build the API server
	$(GO) build -o $(API_BIN) ./cmd/pgfleet-api

.PHONY: cli
cli: ## Build the standalone disaster-recovery CLI (bin/pgfleet)
	$(GO) build -o $(BIN_DIR)/pgfleet ./cmd/pgfleet

.PHONY: run
run: build ## Run the API server, loading .env (copy .env.example first)
	@set -a; [ -f .env ] && . ./.env; set +a; $(API_BIN)

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

PG_VERSIONS ?= 13 14 15 16 17

.PHONY: image
image: ## Build the default managed postgres+pgBackRest image (PG 16)
	docker build -t pgfleet/postgres-pgbackrest:16 docker/postgres-pgbackrest

.PHONY: images
images: ## Build the managed image for every supported PG version (13–17)
	@for v in $(PG_VERSIONS); do \
		echo "==> building pgfleet/postgres-pgbackrest:$$v"; \
		docker build --build-arg PG_VERSION=$$v -t pgfleet/postgres-pgbackrest:$$v docker/postgres-pgbackrest || exit 1; \
	done

.PHONY: dev-up
dev-up: ## Start dev dependencies (meta-DB + MinIO)
	docker compose -f deploy/docker-compose.yml up -d

.PHONY: dev-down
dev-down: ## Stop dev dependencies
	docker compose -f deploy/docker-compose.yml down

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
