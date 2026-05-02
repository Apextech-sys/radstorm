# radstorm E2E Test Suite

Operator instructions for the end-to-end test harness that runs the full radstorm stack
against the local Docker FreeRADIUS rig.

## Overview

The E2E suite lives in `test/e2e/` and exercises the complete radstorm system:

| Script | What it tests |
|---|---|
| `scenarios/smoke-100.sh` | CLI runs 100-subscriber scenario; asserts summary.json fields and Parquet files |
| `scenarios/cli-flows.sh` | validate-config, single-auth, run-scenario, analyze-results subcommands |
| `scenarios/api-flow.sh` | REST API: POST /runs, SSE events stream, GET /results |

## Prerequisites

- Docker Desktop (or Docker Engine) with Compose v2+
- Go 1.22+
- jq
- curl
- make

All must be in PATH. The orchestrator (`run.sh`) checks and fails early if any are missing.

## Quick start

```bash
# From repo root:
bash test/e2e/run.sh 2>&1 | tee /tmp/e2e-output.txt
```

Or via make:

```bash
make e2e
```

The script:
1. Verifies prerequisites
2. Builds the `parquet-row-count` assertion utility (Go, uses project module)
3. Brings up Docker FreeRADIUS (`docker compose up -d`), waits for health
4. Runs each scenario in order
5. Tears down Docker (`docker compose down -v`)
6. Exits with 0 (all pass), 1 (failures), 2 (blocked — binary missing), or 3 (prereq missing)

## Exit code semantics

| Code | Meaning |
|---|---|
| 0 | All scenarios passed |
| 1 | One or more assertion failures |
| 2 | One or more scenarios blocked — CLI or API binary not built (parallel wave not merged yet) |
| 3 | Prerequisite check failed (missing tool) |

## Running individual scenarios

Each scenario script can be run standalone — the Docker rig must already be up:

```bash
# Bring up FreeRADIUS first
make docker-up

# Then run a single scenario
bash test/e2e/scenarios/smoke-100.sh
bash test/e2e/scenarios/cli-flows.sh
bash test/e2e/scenarios/api-flow.sh
```

## Wave 3 status — what is blocked

The E2E harness was authored in Wave 3D. Waves 3A (CLI) and 3B (API) were running
in parallel. If the CLI binary (`bin/radstorm`) or the API binary (`bin/radstorm-api`)
are not present, scenarios will exit with code 2 (BLOCKED) rather than silently skipping.

Once the orchestrator merges 3A and 3B into this worktree and runs `make build`, all
scenarios should pass against the Docker FreeRADIUS rig.

**Expected post-merge pass state:**

- `smoke-100.sh`: 100 established, 0 failures, Parquet row count == 100
- `cli-flows.sh`: all 5 subcommand flows pass
- `api-flow.sh`: 201 on POST /runs, SSE progress events received, results == 100 established

## FreeRADIUS rig

Defined in `test/docker/docker-compose.yml`. Serves on host ports:
- Auth: `127.0.0.1:11812`
- Acct: `127.0.0.1:11813`
- Shared secret: `testing123`
- Seeded users: `sub00000001/pw00000001` ... `sub00005000/pw00005000`

## Assertion utilities

`test/e2e/assertions.sh` provides bash functions:

- `assert_jq_eq <file> <expr> <expected>` — jq field equality
- `assert_jq_lt <file> <expr> <threshold>` — numeric less-than
- `assert_jq_ne <file> <expr> <unexpected>` — jq field not equal
- `assert_jq_ge <file> <expr> <threshold>` — numeric greater-or-equal
- `assert_file_exists <path>` — file exists and non-empty
- `assert_exit_code <actual> <expected>` — exit code comparison
- `assert_exit_nonzero <actual>` — non-zero exit
- `assert_string_contains <haystack> <needle>` — substring
- `assert_parquet_rows <binary> <file> <count>` — Parquet row count via parquet-row-count util

## parquet-row-count utility

A small Go binary at `test/e2e/parquet-row-count/` that reads a Parquet file footer and
prints the row count. Built automatically by `run.sh` or manually:

```bash
cd /path/to/repo
go build -o test/e2e/parquet-row-count/parquet-row-count ./test/e2e/parquet-row-count/
```

## CI integration

See `.github/workflows/ci.yml`. The E2E job runs on push to `main` only (not on PRs)
to conserve runner minutes. It needs both `lint-and-test-go` and `lint-and-test-web`
to pass first.
