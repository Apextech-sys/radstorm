# Briefing 3D — E2E test harness

## Mission

Build the end-to-end test harness that runs the full radstorm system against the local Docker FreeRADIUS rig and asserts the outcome. This is the gate that proves the whole stack works together. Plus a basic CI workflow on GitHub Actions.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md`
3. `docs/RUNBOOK.md` (will exist by Wave 4 but reference the docker rig now)
4. `docs/CONVENTIONS.md` — file headers MANDATORY on shell + YAML
5. **`.orchestration/contracts/results-schema.md`** — what to assert against
6. **`.orchestration/briefings/3d-e2e.md`** — this file
7. `test/docker/` — the FreeRADIUS rig (from 1E)
8. `test/fixtures/scenarios/` — the smoke scenarios (from 1E)

## Working directory

- **Worktree:** `C:\dev\radstorm-3d` on branch `wave-3/3d-e2e`
- **Files you own:**
  - `test/e2e/run.sh` — top-level orchestrator: brings up Docker, builds CLI + API, runs scenarios, asserts results
  - `test/e2e/assertions.sh` — JSON assertion helpers (using `jq`)
  - `test/e2e/scenarios/smoke-100.sh` — runs the 100-subscriber smoke scenario and asserts
  - `test/e2e/scenarios/cli-flows.sh` — runs validate-config, single-auth, run-scenario, analyze-results in sequence and asserts each
  - `test/e2e/scenarios/api-flow.sh` — starts the API server, drives a run via curl + SSE, asserts results
  - `test/e2e/README.md` — operator instructions
  - `.github/workflows/ci.yml` — CI workflow that runs unit tests + (best-effort) the e2e suite

## Scope

**In scope:**
- `test/e2e/run.sh`:
  - Pre-check: Docker, Go, Node, jq, curl available; CLI binary built or buildable
  - Bring up FreeRADIUS rig with `docker compose -f test/docker/docker-compose.yml up -d`, wait for health
  - Run each scenario in `test/e2e/scenarios/`, capturing pass/fail
  - Tear down rig at end
  - Exit code = max of all scenario exit codes
- `test/e2e/scenarios/smoke-100.sh`:
  - Build CLI: `make build-cli`
  - Run: `./bin/radstorm run-scenario --config test/fixtures/scenarios/smoke-100.toml --out /tmp/radstorm-e2e/smoke-100`
  - Assert exit code 0
  - Assert `summary.json.subscribers.established == 100`
  - Assert `summary.json.subscribers.auth_failed == 0`
  - Assert `summary.json.subscribers.acct_failed == 0`
  - Assert `summary.json.outcome == "succeeded"`
  - Assert `summary.json.duration_ms < 30000` (should complete in well under 30s)
  - Assert events.parquet exists and is non-empty
  - Assert subscribers.parquet exists and has 100 rows (use a small Go test util OR `python -c "import pyarrow.parquet as pq; print(pq.read_table('subscribers.parquet').num_rows)"` if Python available — recommend a small Go util)
- `test/e2e/scenarios/cli-flows.sh`:
  - `radstorm validate-config test/fixtures/scenarios/smoke-100.toml` → expect exit 0
  - `radstorm validate-config /dev/null` → expect non-zero exit + clear error
  - `radstorm single-auth --config test/fixtures/scenarios/smoke-100.toml --user sub00000001 --pass pw00000001` → expect "Access-Accept" in output
  - `radstorm run-scenario` (small one) and then `radstorm analyze-results` on the output dir → expect formatted summary
- `test/e2e/scenarios/api-flow.sh`:
  - Build API: `make build-api`
  - Start: `RADSTORM_API_ADDR=:18080 ./bin/radstorm-api &` (capture pid, trap to kill)
  - `curl -s localhost:18080/api/v1/health` → expect `{"status":"ok"`
  - `curl -s -X POST localhost:18080/api/v1/runs -H 'content-type: application/json' -d @test/e2e/fixtures/api-create-run.json` → capture run id, expect 201
  - Stream SSE for a few seconds, expect `progress` events
  - Wait for run to complete
  - `curl -s localhost:18080/api/v1/runs/<id>/results` → expect summary JSON with established_count == 100
  - Kill API server
- `.github/workflows/ci.yml`:
  - Trigger on push, pull_request to main
  - Job 1: `lint-and-test-go` — Go unit tests + lint
  - Job 2: `lint-and-test-web` — Frontend unit tests + lint + typecheck
  - Job 3: `e2e` (optional, on push to main only) — full e2e on Linux runner with Docker
  - Cache modules between runs

**Out of scope:**
- Performance/scale tests at 1M (require dedicated hardware, not a CI runner)
- Browser-based UI tests (skip Playwright for now to keep things lean)

## Test fixtures

- `test/e2e/fixtures/api-create-run.json` — pre-built body for `POST /runs` containing the smoke-100 config

## Cross-platform notes

- Run on Linux primarily (CI runner is Linux). The smoke scripts should be POSIX bash.
- Local Windows dev: scripts run via Git Bash. Should work, but the API server may need to use `127.0.0.1` instead of `localhost` to dodge IPv6 dualstack issues.

## CI workflow notes

- Use `actions/setup-go@v5` with Go 1.22+ (we use 1.26)
- Use `actions/setup-node@v4` with Node 22+
- Cache `~/go/pkg/mod` and `apps/web/node_modules`
- For the e2e job: `services:` block to bring up FreeRADIUS as a sidecar, OR install Docker on the runner and use docker-compose

## Success criteria

- `bash test/e2e/run.sh` exits 0 on a developer laptop with Docker Desktop running, given the CLI + API are buildable from the worktree state
- CI workflow file is valid (use `act` if installed, or just pass GitHub Actions schema validation)
- Output of `bash test/e2e/run.sh` is captured in your report

## File-header requirement

Headers on every shell script and YAML file you author.

## When you finish

1. `bash test/e2e/run.sh` — must pass on dev laptop. If it fails because of ROUNDS where parallel work hasn't merged yet, document what's blocking and the EXPECTED PASS state once integration completes.
2. Write `.orchestration/reports/3d-e2e.md` with the full output of `bash test/e2e/run.sh`
3. Commit on `wave-3/3d-e2e`:
   ```bash
   git add -A
   git commit -m "test(e2e): full-stack E2E suite + GitHub Actions CI workflow

   Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
   ```
4. Do NOT push or merge.
5. Return concise <200-word summary.
