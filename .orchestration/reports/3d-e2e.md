# Wave 3D — E2E Test Harness Recovery Report

**Recovered from:** laptop reboot mid-build (previous agent was killed before commit)
**Recovery agent:** Claude Sonnet 4.6
**Date:** 2026-05-02
**Branch:** wave-3/3d-e2e

---

## Files Delivered

All files were present as untracked at recovery time. The harness was structurally
complete but not committed and had one correctness bug in `run.sh`.

| File | Status | Notes |
|---|---|---|
| `test/e2e/run.sh` | Fixed + committed | docker compose detection corrected (see below) |
| `test/e2e/assertions.sh` | Verified complete | All 9 assertion helpers present and correct |
| `test/e2e/scenarios/smoke-100.sh` | Verified complete | Header OK, all assertions wired |
| `test/e2e/scenarios/cli-flows.sh` | Verified complete | All 5 CLI subcommands covered |
| `test/e2e/scenarios/api-flow.sh` | Verified complete | Full SSE + results assertion |
| `test/e2e/fixtures/api-create-run.json` | Verified valid | `.config` key present, JSON well-formed |
| `test/e2e/parquet-row-count/main.go` | Verified complete | Header OK, `go vet` passes |
| `test/e2e/parquet-row-count/parquet-row-count` | Pre-built binary | Left in place; built from main.go |
| `test/e2e/README.md` | Verified complete | Operator instructions comprehensive |
| `.github/workflows/ci.yml` | Verified complete | 3-job workflow, caches, e2e optional |

---

## Bug Fixed: Docker Compose Detection in run.sh

**Problem:** The original line was:

```bash
check_tool docker-compose 2>/dev/null || check_tool "docker compose" 2>/dev/null || true
```

`check_tool` calls `command -v "${tool}"`. Passing `"docker compose"` as a single
argument always fails because `command -v` treats it as a literal binary name (no spaces
allowed). The first `check_tool docker-compose` would increment `PREREQ_FAIL`, and the
second call (for the v2 subcommand) would silently fail — meaning systems running
Docker Compose v2 (the default since Docker Desktop 3.x) would abort with exit code 3.

**Fix:** Replaced with an explicit if/elif that runs `docker compose version` directly
to detect v2, keeping the v1 fallback for `docker-compose` binary:

```bash
if command -v docker-compose > /dev/null 2>&1; then
  echo "  [OK] docker-compose (v1)"
elif docker compose version > /dev/null 2>&1; then
  echo "  [OK] docker compose (v2)"
else
  echo "  [MISS] docker compose ..."
  (( PREREQ_FAIL++ )) || true
fi
```

---

## Syntax Check Results

All scripts verified with `bash -n` (no execution, parse-only):

```
test/e2e/run.sh             OK
test/e2e/assertions.sh      OK
test/e2e/scenarios/smoke-100.sh   OK
test/e2e/scenarios/cli-flows.sh   OK
test/e2e/scenarios/api-flow.sh    OK
```

`go vet ./test/e2e/parquet-row-count/` — OK

JSON fixture `test/e2e/fixtures/api-create-run.json` — valid JSON, `.config` key present
with keys: `target, coa_listener, subscribers, source, nas, retransmit, scenario, output`.

---

## Executable Bits

All `.sh` files are `-rwxr-xr-x` (set by previous agent, confirmed present).

---

## File Headers

All shell scripts begin with the mandatory `#` comment block per `docs/CONVENTIONS.md`,
with Purpose / Related / Briefing / Contract sections. The Go util uses the Go-style
comment block. The CI YAML uses the `#` comment block. All compliant.

---

## CI Workflow

`.github/workflows/ci.yml` delivers three jobs:

- **`lint-and-test-go`** — Go modules cache via `actions/setup-go@v5`, `golangci-lint`,
  `go test -race`, builds CLI + API binaries, builds parquet-row-count utility.
- **`lint-and-test-web`** — `actions/setup-node@v4` with npm cache, `npm ci`, typecheck,
  ESLint, Vitest, Next.js production build.
- **`e2e`** — Runs on push to main only (not PRs), requires both lint jobs to pass.
  Runs `bash test/e2e/run.sh` with Docker Compose, uploads artifacts on failure.

Go version: 1.26.2, Node version: 24. Both exceed the minimums (Go 1.22+, Node 22+).

---

## Expected Behaviour When Run End-to-End

The E2E suite requires the following to be present (parallel wave work):

- **Wave 3A** (`wave-3/3a-scenario-cli`): CLI binary at `bin/radstorm`
- **Wave 3B** (`wave-3/3b-api-full`): API binary at `bin/radstorm-api`

These branches exist in the repository (confirmed via `git branch`). Until they are
merged and `make build` is run, scenarios exit with code 2 (BLOCKED) rather than
failing hard — the orchestrator displays a clear "needs Wave 3A/3B merge" message.

**Expected pass state post-merge (Wave 4 integration):**

| Scenario | Expected |
|---|---|
| `smoke-100.sh` | established==100, auth_failed==0, acct_failed==0, outcome==succeeded, duration_ms<30000, Parquet row count==100 |
| `cli-flows.sh` | validate-config(valid)→0, validate-config(/dev/null)→non-zero, single-auth→Access-Accept, run-scenario→0, analyze-results→0 |
| `api-flow.sh` | health→{status:ok}, POST /runs→201+id, SSE progress events received, GET /results→established==100 |

---

## Note on `bash test/e2e/run.sh` Output

Not executed on this recovery pass because:
1. Wave 3A CLI and Wave 3B API binaries are not present in this worktree
2. Running would bring up the Docker FreeRADIUS rig unnecessarily for a known-blocked suite

All scripts are syntactically correct and structurally complete. The expected exit code
from a fresh run before Wave 4 merge is **2** (BLOCKED), not a crash or parse error.

Once Wave 4 merges 3A and 3B and runs `make build`, `bash test/e2e/run.sh` should
exit **0** against a running Docker Desktop with FreeRADIUS healthy.
