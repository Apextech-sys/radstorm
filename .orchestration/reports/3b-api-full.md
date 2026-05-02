# Wave 3B Report — API server full implementation

**Branch:** `wave-3/3b-api-full`
**Recovery context:** picked up an in-progress implementation that was interrupted by a laptop reboot. Substantial work survived (runner, SQLite store, file tailer, fake CLI, handler refactors). This pass finished the missing wiring, repaired the test suite to use the new real-runner architecture, raised coverage to >75%, and smoke-tested the binary.

## What survived from the prior session

- `apps/api/internal/runs/runner.go` — subprocess supervisor (Start, Cancel, Shutdown, watch goroutine that reconciles store state on exit).
- `apps/api/internal/runs/runner_proc_unix.go` / `runner_proc_windows.go` — platform-specific subprocess attributes.
- `apps/api/internal/runs/sqlite_store.go` — full Store implementation backed by `modernc.org/sqlite` (pure Go), with WAL, busy timeout, and orphan reaping at startup.
- `apps/api/internal/runs/tail.go` — polling JSONL tailer for SSE.
- `apps/api/internal/runs/testdata/fakecli/main.go` — fake CLI binary that mirrors the real `radstorm run-scenario` contract.
- `apps/api/internal/runs/testhelpers_test.go` — once-per-process build of the fake CLI.
- `apps/api/internal/server/handlers/runs.go` / `events.go` / `results.go` — already wired to real runner, file-tailing SSE, and on-disk summary.json reads.
- `apps/api/internal/server/router.go` — already accepts `RouterDeps` and constructs all three handlers.
- Modified `go.mod` / `go.sum` (sqlite + toml dependencies pulled in).

## What I finished

1. **Updated `apps/api/cmd/radstorm-api/main.go`** to the new `RouterDeps` API: opens `SQLiteStore` at `<RADSTORM_DATA_DIR>/runs.db`, constructs the `Runner` (resolving the CLI path via `RADSTORM_CLI_BIN` + the FindCLI fallback chain), and calls `runner.Shutdown(10s)` after `srv.Run(ctx)` returns so in-flight subprocesses are drained before the process exits.

2. **Rewrote `apps/api/internal/server/handlers/handlers_test.go`** end-to-end. The old skeleton tests referenced `FakeRunDuration`, `SSEStepInterval`, and the single-arg `NewRunsHandler(store)` — all of which were removed when the fake state machine was deleted. Replaced with a `makeRealRunner(t)` helper that builds a per-test SQLite store + Runner against the fake CLI, plus added explicit coverage for the conflict-409 flow (using `FAKECLI_HANG=1`), real cancellation, results-not-yet-available 409, and the new "no runner configured" 503 path.

3. **Added three new test files in `apps/api/internal/runs/`:**
   - `sqlite_store_test.go` — parity with the in-memory store tests, plus persistence-across-reopen and orphan-reaping-at-startup.
   - `runner_test.go` — success / failure / cancellation flows against the fake CLI; FindCLI fallback chain; default data dir creation.
   - `tail_test.go` — appended-line delivery, file-doesn't-exist-yet polling, stop channel, context cancel, truncation re-open, default interval.

4. **Smoke-tested the binary** (`/tmp/radstorm-api-test`):
   - `GET /api/v1/health` → `{"status":"ok"}` (200)
   - `GET /api/v1/scenarios/templates` → full template list (200)
   - `GET /api/v1/runs` → `{"runs":[]}` (200)
   - SQLite db (`runs.db` + WAL companion files) created on disk.
   - SIGTERM-driven graceful shutdown completed.

## Test results

```
ok  github.com/Apextech-sys/radstorm/apps/api/cmd/radstorm-api          [no test files]
ok  github.com/Apextech-sys/radstorm/apps/api/internal/mockdata         [no test files]
ok  github.com/Apextech-sys/radstorm/apps/api/internal/runs             coverage: 87.9%
ok  github.com/Apextech-sys/radstorm/apps/api/internal/server           coverage: 86.0%
ok  github.com/Apextech-sys/radstorm/apps/api/internal/server/handlers  coverage: 71.0%
ok  github.com/Apextech-sys/radstorm/apps/api/internal/server/middleware coverage: 86.8%
```

`runs` package coverage **87.9%** vs. the briefing's **≥75%** target.

## Manual end-to-end test

Once the real CLI binary lands (Wave 3A) and Docker rig is up:

```bash
export RADSTORM_DATA_DIR=$(mktemp -d)
export RADSTORM_CLI_BIN=$(pwd)/bin/radstorm
go run ./apps/api/cmd/radstorm-api &
API_PID=$!

# POST a real run from the smoke template.
curl -s -X POST localhost:8080/api/v1/runs \
  -H 'Content-Type: application/json' \
  -d @smoke-100-config.json | jq .id

# Watch progress via SSE.
curl -s -N localhost:8080/api/v1/runs/$RUN_ID/events

# Fetch results once status==succeeded.
curl -s localhost:8080/api/v1/runs/$RUN_ID/results | jq

kill $API_PID
```

The fake-CLI based unit tests already exercise this flow (POST → SSE → progress events → complete event → results endpoint returns summary), so the only thing the real-CLI run validates is that the real `radstorm run-scenario` invocation matches the contract. That validation belongs in Wave 4 e2e.

## Notes for downstream waves

- `runs.NewRunner` requires the CLI binary to exist at construction time. If the API server is started without `RADSTORM_CLI_BIN` set and `bin/radstorm` is missing, `main` fatals with `runner_failed`. Wave 4 / production deploy must ensure the binary is in place before launching the API.
- The orphan-reaping behaviour means a server crash does NOT leave a permanently-queued ghost — every `queued|running|cancelling` row is flipped to `failed` with `progress.reason="orphaned (server restart)"` on the next startup.
- Single-tenant gating is enforced at the SQLite store level (`Create` checks `HasActive` under a mutex) so the 409 path is race-free.
