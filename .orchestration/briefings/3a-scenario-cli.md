# Briefing 3A — Scenario driver + CLI binary

## Mission

Build `pkg/scenario` (the driver that orchestrates a full test) and `apps/cli/cmd/radstorm/main.go` (the CLI entrypoint with `run-scenario`, `validate-config`, `single-auth`, `analyze-results` subcommands). The scenario driver wires together: subscriber pool + I/O engine + collector + server listener. Running `radstorm run-scenario --config ... --out ...` against the Docker FreeRADIUS rig must produce a complete test run end-to-end.

This is the biggest integration slice. Take the time to understand all your dependencies before you start coding.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md` — full system architecture (this slice ties it together)
3. `docs/PROTOCOL.md`
4. `docs/CONFIG.md`
5. `docs/CONVENTIONS.md` — file headers MANDATORY
6. **All four contracts** in `.orchestration/contracts/`
7. **`.orchestration/briefings/3a-scenario-cli.md`** — this file
8. All previously-merged packages: `pkg/radius`, `pkg/config`, `pkg/events`, `pkg/collector`, `pkg/io`, `pkg/subscriber`, `pkg/server`. Read each package's main `.go` files to understand their API.

## Working directory

- **Worktree:** `C:\dev\radstorm-3a` on branch `wave-3/3a-scenario-cli`
- **Files you own:**
  - **REFACTOR:** `pkg/subscriber/sender.go` — DELETE. The local Sender/RetransmitPolicy/SendResult types must be removed; subscriber must import them from `pkg/io` instead. Update all usages in `pkg/subscriber/` and any tests.
  - `pkg/scenario/schedule.go` — activation schedule generation: Gaussian (cold_start), Uniform, Pessimal
  - `pkg/scenario/lifecycle.go` — Phase enum (Warmup, Ramp, Drain, Finalize), state transitions
  - `pkg/scenario/runner.go` — the Runner: takes a Config, builds everything, runs to completion, writes results
  - `pkg/scenario/progress.go` — progress.jsonl writer (per-second snapshots for the API to tail)
  - `pkg/scenario/*_test.go`
  - `apps/cli/cmd/radstorm/main.go` — CLI entrypoint
  - `apps/cli/cmd/radstorm/run_scenario.go` — `run-scenario` subcommand
  - `apps/cli/cmd/radstorm/validate_config.go` — `validate-config` subcommand
  - `apps/cli/cmd/radstorm/analyze_results.go` — `analyze-results` subcommand
  - `apps/cli/cmd/radstorm/single_auth.go` — `single-auth` dev subcommand
  - `apps/cli/cmd/radstorm/*_test.go` — integration tests against Docker FreeRADIUS where feasible

## Critical refactor: subscriber.Sender → io.Sender

**Before writing anything else, do this refactor.** During Wave 2, slice 2A (pkg/io) and 2B (pkg/subscriber) defined identical-shaped Sender/RetransmitPolicy/SendResult types in their own packages because they were built in parallel. Now that both are merged, the subscriber package must use the canonical types from pkg/io.

Specifically:
1. In `pkg/subscriber/sender.go`: delete the file
2. In `pkg/subscriber/subscriber.go` (and any other consumers): replace `type Sender interface{...}` references with `io.Sender` (defined in pkg/io). Replace `RetransmitPolicy` with `io.RetransmitPolicy`. Replace `SendResult` with `io.SendResult`.
3. Update Subscriber.Run and Deps struct accordingly
4. Update tests: the fake Sender used in subscriber tests now implements `io.Sender` (signature should still match)
5. Re-run `go test ./pkg/subscriber/...` — must still pass

If pkg/io's interface is missing methods or has different fields than pkg/subscriber expected, prefer pkg/io's version (it's the real implementation) and adjust subscriber accordingly.

## Scenario driver — public API

```go
package scenario

type Runner struct{ /* ... */ }

type Opts struct {
    Config    *config.Config
    Creds     []config.Credential
    OutputDir string
    Logger    *slog.Logger
}

func New(opts Opts) (*Runner, error)
func (r *Runner) Run(ctx context.Context) error  // blocks until done; ctx cancel = drain mode

// Internally constructs:
//   - Collector (writes to OutputDir)
//   - io.Engine (with source IPs, port range, collector as event sink, server listener as ServerHandler)
//   - subscriber.Pool (from creds + config)
//   - server.Listener (with subscriber.Pool as Lookup)
//   - schedule (per scenario type)
// Then runs the lifecycle.
```

## Activation schedules

- **cold_start (Gaussian):** Generate N activation times. Each subscriber's offset = clamp(N(μ_sec, σ_sec), [μ-3σ, μ+3σ]) seconds from T0. Sort by offset. Use `math/rand` with seed from config (or default).
- **uniform:** offset_i = i * (DurationSec / N). Last subscriber starts at DurationSec.
- **pessimal:** offset_i = (i / N) * (BurstWindowMs / 1000). All subscribers within BurstWindowMs.
- **coa_storm:** subscribers are pre-established; the scenario just runs the listener and waits HardTimeoutSec, capturing CoA traffic.

## Lifecycle phases

- **Warmup (1s):** Listener and engine start. Bind sockets. Verify connectivity. Emit a `lifecycle_warmup` event.
- **Ramp:** Run the activation schedule. For each subscriber's offset, schedule a goroutine that activates the subscriber at that time. Use `time.AfterFunc` or a single-goroutine scheduler.
- **Drain (default 30s, configurable):** Stop new activations. Wait for in-flight subscribers to terminate or for drain timeout. Emit `lifecycle_drain` event.
- **Finalize:** Stop the collector cleanly (which triggers final Parquet flush + summary aggregation + summary.json + summary.txt write). Stop the listener. Stop the engine.

## Progress writer

Background goroutine that, every 1 second during the run, writes a JSON line to `<OutputDir>/progress.jsonl`:

```json
{"offset_ms":1500,"activated":120,"established":95,"failed":0,"in_flight":25,"retransmits":2,"phase":"ramp"}
```

This is what the API server (3B) tails to power SSE.

## CLI subcommands

### `radstorm run-scenario --config <path> --out <dir>`

- Loads config, validates, applies defaults
- Creates output directory
- Writes a copy of the config to `<out>/config.toml`
- Sets up signal handling: SIGINT/SIGTERM → cancel context (triggers drain)
- Calls scenario.Runner.Run(ctx)
- Exit code: 0 on success, 1 on internal error, 2 on threshold-evaluation failure (run completed but didn't meet criteria)

### `radstorm validate-config <path>`

- Loads, validates, dry-runs default expansion
- Optionally pre-flight: try to bind source IPs, send a single Access-Request to target.auth_address, expect any reply within 1s
- Exit code: 0 ok, non-zero on any check failure
- Prints validation results

### `radstorm single-auth --config <path> --user <u> --pass <p>`

Phase-1-style dev tool: authenticates one subscriber against the target server using the config's NAS attributes and shared secret. Logs the round-trip latency. Useful for sanity-checking the rig.

### `radstorm analyze-results <dir>`

- Reads `<dir>/summary.json` and prints summary.txt to stdout
- Optionally re-aggregates from Parquet if summary.json is missing
- Exit code: 0

## Use `cobra` for the CLI

```go
import "github.com/spf13/cobra"
```

## Success criteria

- `go build ./...` succeeds — produces `bin/radstorm` via `make build-cli`
- `go test ./pkg/scenario/... ./apps/cli/... -cover` passes; ≥75% coverage
- Specific tests:
  - schedule.go: cold_start with N=1000 produces 1000 offsets; mean ≈ μ; std ≈ σ; all within [μ-3σ, μ+3σ]
  - schedule.go: uniform produces evenly-spaced offsets
  - schedule.go: pessimal produces all offsets within BurstWindowMs
  - lifecycle.go: phase transitions in correct order
  - runner.go: integration test using fake collector/engine that runs a 10-subscriber scenario end to end
- **Hard E2E gate:** With the Docker FreeRADIUS rig running (`make docker-up`), `./bin/radstorm run-scenario --config test/fixtures/scenarios/smoke-100.toml --out /tmp/radstorm-smoke` succeeds, exits 0, produces summary.json with `established_count == 100`. Run this in your test process if Docker is available, or document the manual steps in your report.

## File-header requirement

Mandatory.

## Notes for the e2e fixture

`test/fixtures/scenarios/smoke-100.toml` already exists (from 1E) and points at `127.0.0.1:11812`/`:11813` with secret `testing123`. It references `test/fixtures/credentials/smoke-100.csv` (also from 1E, 100 entries). You need to make sure the source IP in that fixture is `127.0.0.1` and source ports fit in the unprivileged range. If not, create a new fixture `test/fixtures/scenarios/smoke-100-local.toml`.

## When you finish

1. `export PATH="$PATH:/c/Program Files/Go/bin" && go test ./... -cover -v 2>&1 | tail -40` — must pass
2. `go build ./...` — must compile
3. Build the CLI: `mkdir -p bin && go build -o bin/radstorm ./apps/cli/cmd/radstorm`
4. If Docker available: run `make docker-up && ./bin/radstorm run-scenario --config test/fixtures/scenarios/smoke-100.toml --out /tmp/smoke && cat /tmp/smoke/summary.txt`. Capture output.
5. Write `.orchestration/reports/3a-scenario-cli.md` (include the smoke run output if Docker was available, OR document the steps to run it manually)
6. Commit on `wave-3/3a-scenario-cli`:
   ```bash
   git add -A
   git commit -m "feat(scenario,cli): scenario driver + radstorm CLI; refactor subscriber to use io.Sender

   Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
   ```
7. Do NOT push or merge.
8. Return concise <250-word summary including end-to-end smoke run results.
