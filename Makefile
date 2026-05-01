# radstorm Makefile
# Briefing: orchestrator-managed; minimal targets for build/test/dev

.PHONY: help build build-cli build-api test test-go test-web lint dev clean docker-up docker-down e2e

GO         := go
GOFLAGS    :=
BINDIR     := bin
CLI_OUT    := $(BINDIR)/radstorm
API_OUT    := $(BINDIR)/radstorm-api

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: build-cli build-api ## Build CLI + API binaries

build-cli: ## Build the radstorm CLI
	@mkdir -p $(BINDIR)
	$(GO) build $(GOFLAGS) -o $(CLI_OUT) ./apps/cli/cmd/radstorm

build-api: ## Build the radstorm API server
	@mkdir -p $(BINDIR)
	$(GO) build $(GOFLAGS) -o $(API_OUT) ./apps/api/cmd/radstorm-api

test: test-go test-web ## Run all tests

test-go: ## Run Go unit tests
	$(GO) test -race -count=1 ./...

test-go-integration: ## Run Go integration tests (require Docker rig up)
	$(GO) test -race -count=1 -tags=integration ./...

test-web: ## Run frontend tests
	cd apps/web && npm test -- --run

lint: ## Lint Go and TS
	golangci-lint run ./...
	cd apps/web && npm run lint

dev: ## Start API + frontend dev servers
	@echo "Starting API on :8080 and Next.js on :3000..."
	@(cd apps/web && npm run dev) & \
	(cd apps/api && $(GO) run ./cmd/radstorm-api) & \
	wait

docker-up: ## Bring up Docker FreeRADIUS test rig
	docker compose -f test/docker/docker-compose.yml up -d
	@echo "FreeRADIUS up. Auth on udp/1812, Acct on udp/1813. Shared secret: testing123"

docker-down: ## Tear down Docker test rig
	docker compose -f test/docker/docker-compose.yml down -v

e2e: docker-up build ## Run E2E suite against Docker rig
	./test/e2e/run.sh

clean: ## Remove build artifacts and results
	rm -rf $(BINDIR) results/
	cd apps/web && rm -rf .next out node_modules/.cache

check-headers: ## Verify file-header doc convention
	./scripts/check-headers.sh
