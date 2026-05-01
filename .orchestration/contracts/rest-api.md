# Contract: REST API (apps/api ↔ apps/web)

**Owners:** `apps/api` (Go HTTP handlers + OpenAPI spec at `apps/api/openapi.yaml`), `apps/web/lib/api.ts` (typed client + zod schemas).

**Base URL:** `http://localhost:8080` in dev. Path prefix `/api/v1`.

**Auth:** None (local-only tool).

---

## Endpoints

### `GET /api/v1/health`

Health probe.

**Response 200:**
```json
{ "status": "ok", "version": "0.1.0" }
```

### `GET /api/v1/scenarios/templates`

List built-in scenario templates (smoke, cold-start, uniform, pessimal).

**Response 200:**
```json
[
  { "id": "smoke-100", "name": "Smoke 100", "description": "100 subs, uniform 5s ramp", "config": { ... } },
  { "id": "cold-start-1k", "name": "Cold start 1k", ... }
]
```

### `POST /api/v1/runs`

Trigger a new run.

**Request body:**
```json
{
  "name": "optional human label",
  "config": { /* full Config object per config-schema.md */ }
}
```

**Response 201:**
```json
{
  "id": "run-2026-05-02T01-23-45Z-7f3a",
  "name": "...",
  "status": "queued",
  "created_at": "2026-05-02T01:23:45Z",
  "config": { ... }
}
```

**Errors:**
- `400` — config validation failed (returns array of validation errors)
- `409` — another run is already in progress (single-tenant API)
- `500` — internal

### `GET /api/v1/runs`

List runs (most recent first).

**Query params:** `limit` (default 50), `status` (filter)

**Response 200:**
```json
{
  "runs": [
    { "id": "...", "name": "...", "status": "running", "created_at": "...", "summary": { ... or null } }
  ]
}
```

### `GET /api/v1/runs/{id}`

Get run detail.

**Response 200:**
```json
{
  "id": "...",
  "name": "...",
  "status": "queued | running | succeeded | failed | cancelled",
  "created_at": "...",
  "started_at": "... or null",
  "finished_at": "... or null",
  "config": { ... },
  "progress": {
    "elapsed_ms": 12345,
    "subscribers_total": 1000,
    "subscribers_activated": 450,
    "subscribers_established": 380,
    "subscribers_failed": 5
  },
  "summary": { ... or null until finished — full summary.json }
}
```

### `GET /api/v1/runs/{id}/events?after={offset_ms}`

Server-Sent Events stream of progress updates.

**Event format:**
```
event: progress
data: {"offset_ms":1500,"activated":120,"established":95,"failed":0,"in_flight":25,"retransmits":2}

event: log
data: {"offset_ms":1510,"level":"info","msg":"Wave start"}

event: complete
data: {"summary": {...}}
```

Closes when run completes or client disconnects.

### `POST /api/v1/runs/{id}/cancel`

Cancel a running run (graceful drain).

**Response 200:**
```json
{ "id": "...", "status": "cancelling" }
```

### `GET /api/v1/runs/{id}/results`

Download summary.json directly.

**Response 200:** the full summary.json (see results-schema.md).

### `GET /api/v1/runs/{id}/results/establishment-curve`

Pre-computed time-series for the establishment chart.

**Response 200:**
```json
{
  "interval_ms": 1000,
  "points": [
    { "offset_ms": 0,    "established": 0,   "activated": 0,    "in_flight": 0 },
    { "offset_ms": 1000, "established": 12,  "activated": 18,   "in_flight": 6 },
    ...
  ]
}
```

### `GET /api/v1/runs/{id}/results/latency-histogram`

**Response 200:**
```json
{
  "buckets": [
    { "le_ms": 100,   "count": 850 },
    { "le_ms": 500,   "count": 980 },
    { "le_ms": 1000,  "count": 998 },
    { "le_ms": 5000,  "count": 1000 }
  ]
}
```

### `GET /api/v1/runs/{id}/artifacts`

List downloadable artifacts.

**Response 200:**
```json
{
  "artifacts": [
    { "name": "summary.json", "size": 12345, "url": "/api/v1/runs/{id}/artifacts/summary.json" },
    { "name": "events.parquet", "size": 12345, "url": "..." },
    { "name": "subscribers.parquet", "size": 12345, "url": "..." }
  ]
}
```

### `GET /api/v1/runs/{id}/artifacts/{name}`

Download an artifact file.

---

## Status enum

`queued | running | succeeded | failed | cancelling | cancelled`

A run goes:
- `queued → running` when CLI subprocess starts
- `running → succeeded` when CLI exits 0
- `running → failed` when CLI exits non-zero
- `running → cancelling` after `POST /cancel`
- `cancelling → cancelled` when CLI exits

## Implementation notes for the API server

- One run at a time (single-tenant). Reject `POST /runs` with 409 if another is `queued`/`running`/`cancelling`.
- Spawn the CLI as a subprocess: `radstorm run-scenario --config <tmpfile> --out <run-dir>`
- Stream progress by tailing a `progress.jsonl` file the CLI writes (CLI emits a JSON line per second with current counts; API server tails and re-emits as SSE)
- Persist run metadata in a small SQLite file at `data/runs.db` so runs survive API restarts
- Run directory: `data/runs/<run-id>/` containing the input config, all CLI outputs, and run metadata
