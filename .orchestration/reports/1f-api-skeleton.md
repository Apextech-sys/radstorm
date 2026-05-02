# Report: 1F — Go HTTP API server skeleton

**Branch:** `wave-1/1f-api-skeleton`
**Worktree:** `C:\dev\radstorm-1f`
**Status:** done
**Date:** 2026-05-02

## Summary

Built `apps/api`, the Go HTTP API server skeleton conforming to
`.orchestration/contracts/rest-api.md`. Every endpoint is wired with the
shape promised in the contract. `GET /api/v1/health` is fully real; all
other endpoints serve mocked data so the frontend (slice 1D) can build
against a stable surface. The fake state machine on `POST /runs` makes a
queued run progress to "succeeded" within 2 seconds, complete with progress
snapshots and a hardcoded summary, which lets the frontend exercise its
list / detail / cancel / SSE / results UIs end-to-end before Wave 3 lands.

## What was built

| File | Purpose |
|---|---|
| `apps/api/cmd/radstorm-api/main.go` | Process entry point, signal handling, env-var bind addr |
| `apps/api/internal/server/server.go` | `http.Server` wrapper with graceful shutdown |
| `apps/api/internal/server/router.go` | chi router; mounts every endpoint and the middleware stack |
| `apps/api/internal/server/middleware/middleware.go` | request-id, slog access log, panic recover |
| `apps/api/internal/server/handlers/common.go` | JSON read/write helpers + error envelope |
| `apps/api/internal/server/handlers/health.go` | `/health` |
| `apps/api/internal/server/handlers/scenarios.go` | `/scenarios/templates` |
| `apps/api/internal/server/handlers/runs.go` | `/runs` CRUD + cancel + fake state machine |
| `apps/api/internal/server/handlers/events.go` | `/runs/{id}/events` SSE stream |
| `apps/api/internal/server/handlers/results.go` | `/runs/{id}/results*`, artifacts |
| `apps/api/internal/runs/store.go` | `Store` interface + `MemoryStore` |
| `apps/api/internal/config/config_stub.go` | Local Config struct mirror; deleted on Wave 1B merge |
| `apps/api/internal/mockdata/mockdata.go` | Templates, summary, curve, histogram, progress sequence |
| `apps/api/openapi.yaml` | Full OpenAPI 3.1 spec covering every endpoint and schema |
| `apps/api/internal/.../*_test.go` | Tests across all packages |

## Endpoint status (fake vs real)

| Endpoint | Status | Notes |
|---|---|---|
| `GET /api/v1/health` | **real** | Returns `{"status":"ok","version":"0.1.0-skeleton"}` |
| `GET /api/v1/scenarios/templates` | **mock (stable)** | 4 hardcoded templates: smoke-100, cold-start-1k, uniform-1k, pessimal-1k |
| `POST /api/v1/runs` | **mock state machine** | Validates config (skeleton-level), persists to in-memory store, kicks off goroutine that drives queued → running → succeeded over `FakeRunDuration` (2s); 409 if a run is already active; 400 with field-level details on invalid config |
| `GET /api/v1/runs` | **real (against in-memory store)** | Honours `?limit` and `?status` |
| `GET /api/v1/runs/{id}` | **real (against in-memory store)** | 404 if missing; serves run as the lifecycle progresses |
| `POST /api/v1/runs/{id}/cancel` | **real (in-memory)** | Flips status to `cancelling`, then `cancelled` 200ms later; 404 / 409 on bad state |
| `GET /api/v1/runs/{id}/events` | **mock** | SSE: 1 `log` + 8 `progress` (250ms apart) + 1 `complete` carrying the canned summary; cancels cleanly when client disconnects |
| `GET /api/v1/runs/{id}/results` | **mock** | Returns the run's stored summary if present, else the canned summary (so the UI can be built before runs actually finish) |
| `GET /api/v1/runs/{id}/results/establishment-curve` | **mock** | 9-point hardcoded curve |
| `GET /api/v1/runs/{id}/results/latency-histogram` | **mock** | 4-bucket hardcoded histogram |
| `GET /api/v1/runs/{id}/artifacts` | **mock (empty)** | Returns `{"artifacts":[]}`; Wave 3 reads the run dir |
| `GET /api/v1/runs/{id}/artifacts/{name}` | **mock (always 404)** | Route exists so frontend hrefs don't break |

All shapes match the JSON in `.orchestration/contracts/rest-api.md` and
`.orchestration/contracts/results-schema.md` exactly.

## Cross-cutting

- **Routing:** `github.com/go-chi/chi/v5`
- **CORS:** `github.com/go-chi/cors` permitting `http://localhost:3000` (preflight verified by test)
- **Logging:** stdlib `slog` JSON to stdout, with request-id propagation
- **Recover:** panics → JSON 500, never crash the process
- **Bind address:** `:8080` default, override via `RADSTORM_API_ADDR`
- **Graceful shutdown:** `signal.NotifyContext` on `SIGINT`/`SIGTERM`; 10 s shutdown grace
- **Read timeouts:** 60 s / WriteTimeout 0 (required for SSE)

## Coordination notes for the orchestrator

- **Wave 1B (`pkg/config`):** I depended on `apps/api/internal/config/config_stub.go` — a minimal struct-only mirror of the canonical config schema. **Action on merge:** delete `apps/api/internal/config/` and switch the three handlers + the runs store to import the real `github.com/Apextech-sys/radstorm/pkg/config`. The exported names (`Config`, `Validate`, `ValidationError`) match the names I used, so the diff is mostly the import path. Validation in the stub is a minimal "required fields present" check; the real package's stricter validator will simply tighten the 400 envelope.
- **Wave 3 (real API):** `runs.Store` is an interface — the in-memory impl is replaced by SQLite without touching handlers. The `progressFakeRun` goroutine in `runs.go` and the SSE step loop in `events.go` are the two places to delete when wiring the real CLI subprocess + `progress.jsonl` tailer.

## Verification

```bash
$ go build ./apps/api/...
ok

$ go test ./apps/api/... -cover
ok  github.com/Apextech-sys/radstorm/apps/api/internal/runs                94.8%
ok  github.com/Apextech-sys/radstorm/apps/api/internal/server              86.0%
ok  github.com/Apextech-sys/radstorm/apps/api/internal/server/handlers     82.6%
ok  github.com/Apextech-sys/radstorm/apps/api/internal/server/middleware   86.8%
```

Coverage exceeds the 70% bar set in the briefing on every package.

```bash
$ RADSTORM_API_ADDR=:8080 go run ./apps/api/cmd/radstorm-api &
{"level":"INFO","msg":"radstorm_api_starting","addr":":8080"}
{"level":"INFO","msg":"api_server_listening","addr":":8080"}

$ curl -s localhost:8080/api/v1/health
{"status":"ok","version":"0.1.0-skeleton"}

$ curl -s localhost:8080/api/v1/scenarios/templates | jq 'length'
4

$ curl -s localhost:8080/api/v1/runs
{"runs":[]}
```

## File-header compliance

Every Go file in `apps/api/` (including test files) carries the
package-comment header documented in `docs/CONVENTIONS.md`. `openapi.yaml`
carries the equivalent YAML-comment header.
