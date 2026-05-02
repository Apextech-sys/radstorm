# Briefing 3B — API server full implementation

## Mission

Replace the Wave 1F API stub's fake state machine with a real implementation: the API server spawns the `radstorm` CLI binary as a subprocess for each run, watches the run's output directory for progress + results, persists run metadata to SQLite, and streams real progress via SSE.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md`
3. `docs/CONVENTIONS.md` — file headers MANDATORY
4. **`.orchestration/contracts/rest-api.md`** — your API surface (frozen)
5. **`.orchestration/contracts/results-schema.md`** — what the CLI produces
6. **`.orchestration/briefings/3b-api-full.md`** — this file
7. `apps/api/` — the existing skeleton from 1F. You're MODIFYING it.
8. The CLI binary at `bin/radstorm` exists after Wave 3A (parallel slice). For development, assume it lives at `bin/radstorm` relative to the API server's working directory.

## Working directory

- **Worktree:** `C:\dev\radstorm-3b` on branch `wave-3/3b-api-full`
- **Files you modify/create:**
  - `apps/api/internal/runs/store.go` — replace MemoryStore with SQLite-backed store (using `github.com/mattn/go-sqlite3` or pure-Go `modernc.org/sqlite` to avoid CGO)
  - `apps/api/internal/runs/runner.go` — NEW: spawns CLI subprocess, watches progress.jsonl, watches summary.json
  - `apps/api/internal/runs/store_test.go` — update for SQLite-backed store
  - `apps/api/internal/server/handlers/runs.go` — wire to real runner
  - `apps/api/internal/server/handlers/events.go` — replace fake SSE with real progress.jsonl tail + complete event
  - `apps/api/internal/server/handlers/results.go` — read real summary.json from disk
  - `apps/api/cmd/radstorm-api/main.go` — config: data dir, CLI binary path

## Scope

**In scope:**
- SQLite store at `data/runs.db` (or `RADSTORM_DATA_DIR/runs.db`):
  - schema: `runs(id TEXT PRIMARY KEY, name TEXT, status TEXT, created_at TIMESTAMP, started_at TIMESTAMP, finished_at TIMESTAMP, config_json TEXT, progress_json TEXT)`
  - CRUD methods matching the existing Store interface
  - Use `modernc.org/sqlite` (pure Go, no CGO required on Windows) — strongly preferred
- Runner:
  - `func (r *Runner) Start(runID string, cfg *config.Config) error` — writes config to `data/runs/<id>/config.toml`, spawns `bin/radstorm run-scenario --config <path> --out data/runs/<id>`, captures stdout/stderr to `data/runs/<id>/run.log`, updates store status `queued → running`
  - Watches the subprocess; on exit updates `status → succeeded|failed`, `finished_at`
  - Single-tenant: only one run at a time. Concurrent POST /runs returns 409 if another run is queued/running/cancelling
- Progress streaming:
  - SSE handler tails `data/runs/<id>/progress.jsonl`, emits each line as a `progress` event
  - When summary.json appears AND status is finished: emit `complete` event with summary contents, close stream
  - If client disconnects: stop tailing, don't kill the run
- Results endpoint: serve `summary.json` from disk; pre-compute establishment-curve and latency-histogram on demand by reading summary.json
- Artifact endpoint: list files in `data/runs/<id>/`, allow download
- Cancellation: `POST /runs/{id}/cancel` sends SIGTERM to the subprocess; subprocess does its own drain
- CLI binary path: env `RADSTORM_CLI_BIN` (default `bin/radstorm` resolved via filepath.Abs; fall back to `$PATH` lookup)

**Out of scope:**
- Multi-tenant, queueing, persistence beyond runs metadata
- Authentication

## Critical correctness

- Subprocess management:
  - Use `exec.CommandContext` so cancellation propagates
  - Capture stdout/stderr to a file (NOT to the API process's logs)
  - On the API server's own shutdown: SIGTERM running subprocess, wait briefly, then SIGKILL if needed
  - On API server crash + restart: detect orphaned `running` rows in SQLite, mark them `failed` with reason "orphaned (server restart)"
- File watching:
  - Tail with seek-to-end + periodic stat (no need for fsnotify); 200ms poll interval is fine
  - Handle file-doesn't-exist-yet (CLI may take a second to write progress.jsonl)
- SSE:
  - Set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
  - Flush after every event
  - Heartbeat every 15s (`event: ping\ndata:\n\n`) to prevent proxies from closing idle connections

## Success criteria

- `go test ./apps/api/... -cover` passes; ≥75% coverage
- All existing tests still pass (the test fixtures may need a real CLI binary; for unit tests, use a fake CLI shell script that mimics the output format)
- Manual: with the CLI binary built (Wave 3A) and Docker rig up, POST a real run via curl, watch SSE, fetch results — end-to-end works

## Implementation notes

- For unit tests, write a tiny "fake CLI" Go test helper: builds a binary with `go test -c` that simulates writing progress.jsonl and summary.json files, then exits. Or a bash script wrapper checked into testdata.
- The runner package is the most novel — design it as a clean state machine: `runStarted → runProgress → runComplete | runFailed | runCancelled`.

## File-header requirement

Mandatory.

## When you finish

1. `export PATH="$PATH:/c/Program Files/Go/bin" && go test ./apps/api/... -cover -v 2>&1 | tail -30` — must pass
2. `go build ./...` — must compile
3. Document the manual end-to-end test steps in your report
4. Write `.orchestration/reports/3b-api-full.md`
5. Commit on `wave-3/3b-api-full`:
   ```bash
   git add -A
   git commit -m "feat(api): SQLite store, real subprocess runner, file-tailing SSE, artifact serving

   Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
   ```
6. Do NOT push or merge.
7. Return concise <200-word summary.
