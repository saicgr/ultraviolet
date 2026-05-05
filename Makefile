.PHONY: help build verify test test-unit test-integration test-bq lint format \
        migrate-up migrate-down migrate-roundtrip sqlc dev clean tree

# Default target prints help.
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

## ---- build ----

build: ## Build all binaries (proxy, api, sync) — requires CGo for DuckDB
	@echo "build: stub — wire up after Phase 1 step 1"
	CGO_ENABLED=1 go build -o ./bin/uv-proxy ./cmd/proxy 2>/dev/null || echo "  (cmd/proxy/main.go not yet implemented)"
	CGO_ENABLED=1 go build -o ./bin/uv-api ./cmd/api 2>/dev/null || echo "  (cmd/api/main.go not yet implemented)"
	CGO_ENABLED=1 go build -o ./bin/uv-sync ./cmd/sync 2>/dev/null || echo "  (cmd/sync/main.go not yet implemented)"

## ---- verify (the single check before declaring done) ----

verify: lint test ## Lint + test — single command CI runs

## ---- test ----

test: test-unit ## Run unit tests (no integration)

test-unit: ## Unit tests with race detector
	@echo "test-unit: stub — wire up as packages are implemented"
	CGO_ENABLED=1 go test -race -count=1 -timeout=60s ./... 2>/dev/null || echo "  (no Go test files yet)"

test-integration: ## Integration tests against bigquery-public-data + LocalStack
	@echo "test-integration: stub — requires GOOGLE_APPLICATION_CREDENTIALS + LocalStack"
	@if [ -z "$$GOOGLE_APPLICATION_CREDENTIALS" ]; then \
		echo "  SKIP: GOOGLE_APPLICATION_CREDENTIALS not set"; \
		exit 0; \
	fi
	CGO_ENABLED=1 go test -race -count=1 -timeout=600s -tags=integration ./test/integration/... 2>/dev/null || echo "  (no integration tests yet)"

test-bq: test-integration ## Alias

## ---- lint ----

lint: ## golangci-lint + tsc + shellcheck
	@echo "lint: stub — wire up after .golangci.yml exists"
	@command -v golangci-lint >/dev/null && golangci-lint run --timeout=5m 2>/dev/null || echo "  (golangci-lint missing or no Go files)"
	@command -v shellcheck >/dev/null && find scripts -name '*.sh' 2>/dev/null | xargs -r shellcheck || echo "  (no shell scripts)"
	@if [ -d frontend ] && [ -f frontend/package.json ]; then \
		cd frontend && pnpm tsc --noEmit 2>/dev/null || echo "  (frontend not yet initialized)"; \
	fi

format: ## gofmt + prettier
	gofmt -w .
	@if [ -d frontend ] && [ -f frontend/package.json ]; then \
		cd frontend && pnpm prettier --write . 2>/dev/null || true; \
	fi

## ---- migrations ----

migrate-up: ## Apply all pending migrations
	@echo "migrate-up: stub — wire up after migrations/ has files"
	@command -v migrate >/dev/null && migrate -path ./migrations -database "$$DATABASE_URL" up || echo "  (golang-migrate missing or no migrations)"

migrate-down: ## Roll back one migration
	@command -v migrate >/dev/null && migrate -path ./migrations -database "$$DATABASE_URL" down 1 || echo "  (golang-migrate missing or no migrations)"

migrate-roundtrip: ## up → down → up (proves reversibility)
	@echo "migrate-roundtrip: stub"
	@if [ -z "$$(ls migrations/*.up.sql 2>/dev/null)" ]; then \
		echo "  SKIP: no migrations yet"; \
		exit 0; \
	fi
	$(MAKE) migrate-up
	$(MAKE) migrate-down
	$(MAKE) migrate-up

sqlc: ## Regenerate Go from SQL queries
	@command -v sqlc >/dev/null && sqlc generate || echo "  (sqlc missing or no sqlc.yaml)"

## ---- dev environment ----

dev: ## Start dev services (Postgres, Redis, LocalStack) via docker-compose
	docker compose up -d postgres redis localstack
	@echo "Postgres:   postgres://uv:uv@localhost:5432/uv"
	@echo "Redis:      redis://localhost:6379"
	@echo "LocalStack: http://localhost:4566"
	@echo ""
	@echo "When the proxy is running, connect with:"
	@echo "  psql -h localhost -p 5000 -U <api_key> -d <customer>_<warehouse>"
	@echo "(dev proxy port is 5000 — avoids collision with local Postgres on 5432)"

clean: ## Remove build artifacts
	rm -rf bin/
	rm -rf frontend/dist/
	rm -rf frontend/node_modules/.cache/

tree: ## Show directory layout
	@command -v tree >/dev/null && tree -L 3 -I 'node_modules|.git|.swarm|bin' || find . -maxdepth 3 -not -path '*/.git*' -not -path '*/node_modules*' | sort
