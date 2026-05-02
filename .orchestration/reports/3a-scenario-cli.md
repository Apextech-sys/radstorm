# Wave 3A — Scenario driver + CLI binary (RECOVERY)

Branch: `wave-3/3a-scenario-cli` (NOT pushed)
Worktree: `C:\dev\radstorm-3a`
Status: complete; all tests pass; end-to-end smoke run against Docker FreeRADIUS succeeded.

## Recovery context

The previous agent for this slice was killed by a laptop reboot mid-build. This report documents what survived from that run and what this recovery agent built to finish.

## What survived from the prior run

Already-present uncommitted work the recovery agent did not rewrite:

- `pkg/io/io.go` — added a public `Sender` interface so `pkg/subscriber` can import it (canonical type now lives in pkg/io).
- `pkg/subscriber/sender.go` — DELETED (no longer needed; superseded by `io.Sender`).
- `pkg/subscriber/subscriber.go`, `concurrent_test.go`, `fakes_test.go`, `subscriber_test.go` — refactored to use type aliases (`Sender = io.Sender`, `RetransmitPolicy = io.RetransmitPolicy`, `SendResult = io.SendResult`) instead of locally defined types.
- `pkg/scenario/lifecycle.go` (87 lines) — Phase enum, `phaseOrder()`, `nextPhase()`, `validateTransition()`.
- `pkg/scenario/schedule.go` (212 lines) — `BuildSchedule` for cold_start (Gaussian), uniform, pessimal, coa_storm.
- `go.mod` / `go.sum` — added `github.com/spf13/cobra`.

Verified the refactor by building the whole repo and re-running `go test ./pkg/subscriber/...` — clean pass.

## What this recovery built to finish

### pkg/scenario
- `progress.go` — `liveCounters` (atomic activated/established/failed/retransmits + phase pointer) and `progressWriter` that emits a JSON line per second to `<out>/progress.jsonl`. Snapshot shape matches the briefing's contract.
- `runner.go` — `Runner` type, `Opts`, `New`, `Run(ctx)`. Wires together Collector + io.Engine + subscriber.Pool + server.Listener and drives lifecycle Warmup → Ramp → Drain → Finalize → Done. Supports test injection via `CollectorOverride` / `EngineOverride` and `SkipServerListener`. Computes `config_hash` (sha256:hex) for summary.json. Provides `CollectorAPI` and `EngineAPI` interfaces so unit tests don't need real UDP / parquet.
- `lifecycle_test.go`, `schedule_test.go`, `progress_test.go`, `runner_test.go`, `helpers_test.go` — coverage 79.3% (above the 75% goal).

### apps/cli/cmd/radstorm
- `main.go` — cobra root command, `exitErr` for differentiated exit codes (0 ok / 1 error / 2 threshold-fail), shared `--verbose` flag.
- `run_scenario.go` — `--config`, `--out`, `--seed`, `--drain-seconds`. SIGINT/SIGTERM trigger drain via `signal.NotifyContext`. Mirrors the config TOML to `<out>/config.toml`. Reads back `summary.json` and prints a one-line outcome.
- `validate_config.go` — parse + validate + dry-run schedule. Optional `--preflight` resolves UDP target addresses and reads credentials.
- `single_auth.go` — one Access-Request via `io.Engine.Send`, prints reply code + latency + retransmit count.
- `analyze_results.go` — prints `summary.txt` (or falls back to inline render). `--json` re-emits summary.json pretty-printed.
- `cli_test.go` — covers --help, validate-config success/failure, analyze-results, exitCode helper.

## Build + test results

```
$ go build ./...                     → clean
$ go build -o bin/radstorm.exe ./apps/cli/cmd/radstorm   → clean
$ go test ./... -cover               → all packages PASS

pkg/scenario        coverage: 79.3%
apps/cli/cmd/radstorm coverage: 39.2%   (most CLI work is wiring;
                                          critical paths exercised end-to-end via the smoke run)
pkg/subscriber      coverage: 91.3%   (refactor verified)
pkg/io              coverage: 86.7%
pkg/server          coverage: 80.3%
pkg/collector       coverage: 86.6%
```

Every previously-merged package's tests still pass after the io.Sender refactor.

## End-to-end smoke run (Docker FreeRADIUS available)

`docker ps` confirmed `radstorm-freeradius` already running and healthy on host ports 11812/11813.

The shipped `test/fixtures/scenarios/smoke-100.toml` references `test/fixtures/credentials/smoke-100.csv` whose first line is a `#`-comment header. `pkg/config.LoadCredentials` does not configure `csv.Reader.Comment`, so the loader rejects the file with "missing required column 'username'". This is pre-existing behaviour from slice 1B — out of scope for this recovery to alter.

To demonstrate E2E I authored a temporary `/tmp/smoke-100-local.toml` and ran:

```
./bin/radstorm.exe run-scenario \
  --config /tmp/smoke-100-local.toml \
  --out /tmp/radstorm-smoke
```

Result:
```
run run-1777705832 — outcome=succeeded subscribers=100/100 duration=5668ms
artifacts: C:/Users/dj-st/AppData/Local/Temp/radstorm-smoke
```

`/tmp/radstorm-smoke/summary.json` contains:
```
"subscribers": {
  "total": 100,
  "established": 100,
  "auth_failed": 0,
  ...
}
```

24 `events-shard-*.parquet` files, `subscribers.parquet`, `progress.jsonl`, `config.toml`, `summary.json`, `summary.txt` all written.

`./bin/radstorm.exe analyze-results /tmp/radstorm-smoke` and `./bin/radstorm.exe single-auth --user sub00000001 --pass pw00000001 --method pap` both exited 0 with expected output.

## Recommended follow-up (NOT done in this slice)

- Teach `pkg/config.LoadCredentials` to set `csv.Reader.Comment = '#'` so the shipped `smoke-100.csv` works without massaging. Tiny, isolated change; should be its own PR so this slice's diff stays focused on scenario+CLI.

## Files touched / created (absolute paths)

Modified (recovered from prior agent's uncommitted state):
- `C:\dev\radstorm-3a\pkg\io\io.go`
- `C:\dev\radstorm-3a\pkg\subscriber\subscriber.go`
- `C:\dev\radstorm-3a\pkg\subscriber\concurrent_test.go`
- `C:\dev\radstorm-3a\pkg\subscriber\fakes_test.go`
- `C:\dev\radstorm-3a\pkg\subscriber\subscriber_test.go`
- `C:\dev\radstorm-3a\go.mod`, `C:\dev\radstorm-3a\go.sum`

Deleted (recovered from prior agent's uncommitted state):
- `C:\dev\radstorm-3a\pkg\subscriber\sender.go`

Pre-existed (recovered):
- `C:\dev\radstorm-3a\pkg\scenario\lifecycle.go`
- `C:\dev\radstorm-3a\pkg\scenario\schedule.go`

Created by this recovery:
- `C:\dev\radstorm-3a\pkg\scenario\runner.go`
- `C:\dev\radstorm-3a\pkg\scenario\progress.go`
- `C:\dev\radstorm-3a\pkg\scenario\runner_test.go`
- `C:\dev\radstorm-3a\pkg\scenario\schedule_test.go`
- `C:\dev\radstorm-3a\pkg\scenario\lifecycle_test.go`
- `C:\dev\radstorm-3a\pkg\scenario\progress_test.go`
- `C:\dev\radstorm-3a\pkg\scenario\helpers_test.go`
- `C:\dev\radstorm-3a\apps\cli\cmd\radstorm\main.go`
- `C:\dev\radstorm-3a\apps\cli\cmd\radstorm\run_scenario.go`
- `C:\dev\radstorm-3a\apps\cli\cmd\radstorm\validate_config.go`
- `C:\dev\radstorm-3a\apps\cli\cmd\radstorm\single_auth.go`
- `C:\dev\radstorm-3a\apps\cli\cmd\radstorm\analyze_results.go`
- `C:\dev\radstorm-3a\apps\cli\cmd\radstorm\cli_test.go`
- `C:\dev\radstorm-3a\bin\radstorm.exe` (build artifact; .gitignored if appropriate)
- `C:\dev\radstorm-3a\.orchestration\reports\3a-scenario-cli.md` (this file)
