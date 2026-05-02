# ADR 0001 — Go monorepo with Next.js as a sibling project

<!-- Purpose: Documents the decision to use a single repository with one Go module at the root and a separate Node project under apps/web. -->

**Status:** Accepted

**Date:** 2026-05-02

---

## Context

radstorm has two distinct runtime environments: a Go backend (CLI test engine and HTTP API server) and a React/Next.js frontend. A repository structure decision was needed before any code was written.

The options were:

1. Single repository, one Go module at root, Next.js under `apps/web/` as a separate Node project
2. Separate repositories (Go monorepo in one, Next.js in another)
3. Go module-per-component (multiple `go.mod` files in one repo)

The primary constraints were:

- **Overnight autonomous build**: sub-agents needed unambiguous ownership boundaries. Ambiguous layouts (like interleaved Go and TypeScript trees) slow agents down.
- **Single team / single deploy**: there is no organizational reason to split into separate repos. The frontend and backend always version-lock together.
- **Windows development host**: the build system needed to work on Windows without exotic tooling. WSL-based monorepo tooling (Nx, Turborepo, Bazel) adds surface area that is hard to debug under autonomous conditions.

---

## Decision

One Git repository. One `go.mod` at the repository root covering all Go code. `apps/web/` is a standard Next.js project with its own `package.json` — a sibling to the Go packages, not nested inside the Go module.

```
/
  go.mod                      ← single Go module (github.com/Apextech-sys/radstorm)
  go.sum
  Makefile                    ← coordinates both build systems
  apps/
    cli/cmd/radstorm/         ← Go CLI binary
    api/cmd/radstorm-api/     ← Go API server binary
    web/                      ← Next.js project (own package.json, not a Go package)
  pkg/                        ← shared Go packages
  test/
    e2e/                      ← shell-based E2E harness
    docker/                   ← FreeRADIUS test rig
```

The Makefile targets (`make build`, `make dev`, `make test`) coordinate both `go build` and `npm` commands without requiring a polyglot build tool.

---

## Consequences

**Positive:**

- Sub-agents can claim clear file ownership: Go agents own `pkg/`, `apps/cli/`, `apps/api/`; frontend agents own `apps/web/`. No cross-environment file conflicts.
- `go build ./...` from the root builds every Go package. `cd apps/web && npm run build` builds the frontend. No special tooling required.
- CI is straightforward: two separate jobs (`lint-and-test-go`, `lint-and-test-web`) with no shared build graph to manage.
- A single `git clone` gives a developer everything they need. No submodule headaches.

**Negative / trade-offs:**

- `go build ./...` at the root does not build the frontend. This is a natural expectation mismatch for Go developers who expect one command to build everything. The Makefile's `build` target fills this gap.
- The `apps/web/` directory contains a large `node_modules/` tree that Go tools scan unnecessarily (e.g., `go vet ./...` before module graph awareness). `.gitignore` handles `node_modules/` but `go vet` still needs explicit path exclusion in some tooling configurations.
- Shared types between Go and TypeScript must be maintained manually (the contract files in `.orchestration/contracts/` are the source of truth; TypeScript types mirror Go structs by hand). There is no code-generation pipeline to keep them in sync automatically.

---

## Alternatives considered

### Separate repositories

Rejected. The frontend and backend version-lock completely — there is no scenario where the API server runs at one version while the frontend runs at another. Separate repos would require coordinating releases across two repositories for every change, adding process overhead with no benefit at this scale.

### Multiple Go modules

Rejected. Multiple `go.mod` files (one per `apps/` subdirectory) would complicate `go work` workspace setup and require every sub-agent to manage workspace files. The shared `pkg/` packages would need to be versioned independently or referenced via `replace` directives, which is fragile in an autonomous build environment.

### Polyglot build tool (Nx, Turborepo)

Rejected. These tools add a significant dependency and learning surface. They require their own configuration files, conflict with the Makefile-centric build model, and are hard to debug in a headless autonomous build context. The Makefile is sufficient for the build graph complexity at hand.
