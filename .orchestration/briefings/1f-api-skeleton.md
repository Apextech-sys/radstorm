# Briefing 1F — Go HTTP API server skeleton

## Mission

Build the `apps/api` Go HTTP API server skeleton conforming to `.orchestration/contracts/rest-api.md`. Stub handlers (returning `501 Not Implemented` or simple mocked data) for every endpoint. The full implementation comes in Wave 3, but the routing, middleware, OpenAPI spec, and shape must exist now so the frontend (slice 1D) can wire its API client to a real surface.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/CONVENTIONS.md`
3. **`.orchestration/contracts/rest-api.md`** — the surface to implement
4. `docs/ARCHITECTURE.md`

## Working directory

- **Worktree:** `C:\dev\radstorm` on `main` (no parallel writers to `apps/api/`)
- **Files you own:**
  - `apps/api/cmd/radstorm-api/main.go`
  - `apps/api/internal/server/server.go`
  - `apps/api/internal/server/router.go`
  - `apps/api/internal/server/handlers/*.go` — one file per endpoint group (health, runs, results)
  - `apps/api/internal/server/middleware/*.go` — logging, recover, CORS for localhost:3000
  - `apps/api/openapi.yaml` — full OpenAPI 3.1 spec mirroring rest-api.md
  - `apps/api/internal/runs/store.go` — interface + in-memory implementation (real SQLite store comes in Wave 3)
  - `apps/api/*_test.go`

## Scope

**In scope:**
- Use `chi` (`github.com/go-chi/chi/v5`) as the router
- Use `slog` for structured logging
- CORS middleware permitting `http://localhost:3000`
- Recover middleware that returns 500 + JSON error
- Request-ID middleware
- `GET /api/v1/health` — fully working
- `GET /api/v1/scenarios/templates` — return at least 3 hardcoded templates (smoke-100, cold-start-1k, uniform-1k) using the schema from `config-schema.md`
- `POST /api/v1/runs` — accept config, validate it (call `pkg/config` validator), if valid return a fake `run` object with `status: "queued"` and store in the in-memory store. Wire the run to "succeed" 2 seconds later for now (fake state machine for frontend testing).
- `GET /api/v1/runs` and `GET /api/v1/runs/{id}` — read from the in-memory store
- `POST /api/v1/runs/{id}/cancel` — flips status to `cancelled` in the store
- `GET /api/v1/runs/{id}/events` — SSE stream that emits 5–10 fake progress events over a few seconds, then `complete` event with a hardcoded summary
- `GET /api/v1/runs/{id}/results` — return the same hardcoded summary
- `GET /api/v1/runs/{id}/results/establishment-curve` — return a hardcoded curve
- `GET /api/v1/runs/{id}/results/latency-histogram` — return a hardcoded histogram
- `GET /api/v1/runs/{id}/artifacts` — empty list for now
- OpenAPI spec at `apps/api/openapi.yaml` covering everything above
- Server listens on `:8080` (configurable via `RADSTORM_API_ADDR` env var)
- Graceful shutdown on SIGTERM/SIGINT

**Out of scope:**
- Spawning the real CLI (Wave 3)
- SQLite persistence (Wave 3)
- Real Parquet reading (Wave 3)

## Success criteria

- `go build ./apps/api/...` succeeds
- `go test ./apps/api/...` passes; ≥70% coverage on handlers
- `go run ./apps/api/cmd/radstorm-api` starts on :8080
- `curl localhost:8080/api/v1/health` returns `{"status":"ok"...}`
- `curl localhost:8080/api/v1/scenarios/templates` returns the templates
- Hitting `POST /api/v1/runs` with a valid config from `pkg/config` validation returns 201
- The frontend can drive its UI against this fake API and see plausible behavior end-to-end

## Test requirements

- Use `httptest` for handler tests
- One test per endpoint asserting status code + response shape
- One test that POSTs an invalid config and expects 400 with validation errors

## File-header requirement

Mandatory per CONVENTIONS.md.

## Dependencies you may add

- `github.com/go-chi/chi/v5`
- `github.com/go-chi/cors`
- (`pkg/config` is internal — depend on it)
- `github.com/stretchr/testify`

## Reporting

Write `.orchestration/reports/1f-api-skeleton.md` per template. List all endpoints implemented and their current "fake vs real" status.
