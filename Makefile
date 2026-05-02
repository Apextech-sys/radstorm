# radstorm Makefile
# Briefing: orchestrator-managed; minimal targets for build/test/dev
# Wave 6: added build-all, build-cross, checksums, release-local targets

.PHONY: help build build-all build-cli build-api build-cross checksums release-local \
        test test-go test-web lint dev clean docker-up docker-down e2e check-headers

GO         := go
GOFLAGS    :=
BINDIR     := bin
DISTDIR    := dist
CLI_OUT    := $(BINDIR)/radstorm
API_OUT    := $(BINDIR)/radstorm-api

# Version string: use git describe if available, else "dev"
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Release ldflags: strip debug info + embed version
RELEASE_LDFLAGS := -s -w -X main.Version=$(VERSION)

# Cross-compilation targets: GOOS/GOARCH/suffix triples
PLATFORMS := \
  linux/amd64/ \
  linux/arm64/ \
  darwin/amd64/ \
  darwin/arm64/ \
  windows/amd64/.exe

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: build-cli build-api ## Build CLI + API binaries for the current OS/arch (alias: build-all)

build-all: build ## Alias for build — build both binaries for current OS/arch

build-cli: ## Build the radstorm CLI
	@mkdir -p $(BINDIR)
	$(GO) build $(GOFLAGS) -o $(CLI_OUT) ./apps/cli/cmd/radstorm

build-api: ## Build the radstorm API server
	@mkdir -p $(BINDIR)
	$(GO) build $(GOFLAGS) -o $(API_OUT) ./apps/api/cmd/radstorm-api

# Cross-compile both binaries for all 5 OS/arch combinations into dist/.
# Naming: dist/radstorm-{os}-{arch}[.exe]
build-cross: ## Build for all 5 OS/arch combos into dist/
	@mkdir -p $(DISTDIR)
	@echo "Building radstorm $(VERSION) for all platforms..."
	@for platform in $(PLATFORMS); do \
	  GOOS=$$(echo $$platform | cut -d/ -f1); \
	  GOARCH=$$(echo $$platform | cut -d/ -f2); \
	  SUFFIX=$$(echo $$platform | cut -d/ -f3); \
	  CLI_DEST=$(DISTDIR)/radstorm-$${GOOS}-$${GOARCH}$${SUFFIX}; \
	  API_DEST=$(DISTDIR)/radstorm-api-$${GOOS}-$${GOARCH}$${SUFFIX}; \
	  echo "  -> $${GOOS}/$${GOARCH}"; \
	  CGO_ENABLED=0 GOOS=$${GOOS} GOARCH=$${GOARCH} \
	    $(GO) build -ldflags="$(RELEASE_LDFLAGS)" -o $${CLI_DEST} ./apps/cli/cmd/radstorm || exit 1; \
	  CGO_ENABLED=0 GOOS=$${GOOS} GOARCH=$${GOARCH} \
	    $(GO) build -ldflags="$(RELEASE_LDFLAGS)" -o $${API_DEST} ./apps/api/cmd/radstorm-api || exit 1; \
	done
	@echo "Cross-build complete. Artifacts in $(DISTDIR)/:"
	@ls -lh $(DISTDIR)/

checksums: ## Generate dist/SHA256SUMS for all binaries in dist/
	@[ -d "$(DISTDIR)" ] || { echo "Run 'make build-cross' first."; exit 1; }
	@cd $(DISTDIR) && sha256sum * > SHA256SUMS
	@echo "Checksums written to $(DISTDIR)/SHA256SUMS:"
	@cat $(DISTDIR)/SHA256SUMS

release-local: build-cross checksums ## Dry-run release: cross-build + checksums + list dist/
	@echo ""
	@echo "=== Local release dry-run complete ==="
	@echo "Version : $(VERSION)"
	@echo "Artifacts in $(DISTDIR)/:"
	@ls -lh $(DISTDIR)/
	@echo ""
	@echo "To publish: git tag v<x.y.z> && git push --tags"

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
