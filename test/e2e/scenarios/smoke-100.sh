#!/usr/bin/env bash
# Smoke-100 scenario: runs 100 subscribers against Docker FreeRADIUS and asserts results.
#
# Purpose:
#   Builds the radstorm CLI binary (via `make build-cli`), runs the smoke-100 scenario
#   config against the local Docker FreeRADIUS rig, then asserts summary.json fields
#   match expected values (100 established, 0 failures, outcome == succeeded, etc.).
#   Also asserts that events.parquet and subscribers.parquet are present and non-empty,
#   and that subscribers.parquet has exactly 100 rows.
#
# Related:
#   - test/fixtures/scenarios/smoke-100.toml (scenario config)
#   - test/fixtures/credentials/smoke-100.csv (credentials)
#   - test/docker/docker-compose.yml (FreeRADIUS rig)
#   - test/e2e/assertions.sh (assertion helpers)
#   - .orchestration/contracts/results-schema.md (summary.json schema)
#
# Briefing: .orchestration/briefings/3d-e2e.md
# Contract: exits 0 on success, non-zero on failure

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
E2E_DIR="${REPO_ROOT}/test/e2e"
PARQUET_COUNT="${E2E_DIR}/parquet-row-count/parquet-row-count"
OUT_DIR="/tmp/radstorm-e2e/smoke-100"
SUMMARY="${OUT_DIR}/summary.json"
CLI="${REPO_ROOT}/bin/radstorm"

# shellcheck source=test/e2e/assertions.sh
source "${E2E_DIR}/assertions.sh"

# ── Colour / banner ───────────────────────────────────────────────────────────
echo ""
echo "${CYAN}=== Scenario: smoke-100 ===${RESET}"
echo "  Output dir: ${OUT_DIR}"
echo ""

assert_reset

# ── Gate: CLI binary must exist ───────────────────────────────────────────────
if [ ! -f "${CLI}" ]; then
  echo "${RED}[BLOCKED]${RESET} CLI binary not found at ${CLI}"
  echo "  Expected after Wave 3A merge. Building now..."
  (cd "${REPO_ROOT}" && make build-cli 2>&1) || {
    echo ""
    echo "${RED}[BLOCKED]${RESET} make build-cli failed."
    echo "  This scenario requires Wave 3A (apps/cli/) to be merged."
    echo "  Once orchestrator merges wave-3/3a-scenario into this worktree,"
    echo "  re-run: bash test/e2e/run.sh"
    exit 2
  }
fi

# Verify binary is executable
if [ ! -x "${CLI}" ]; then
  echo "${RED}[FAIL]${RESET} ${CLI} exists but is not executable"
  exit 1
fi

# ── Prepare output dir ────────────────────────────────────────────────────────
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

# ── Run scenario ──────────────────────────────────────────────────────────────
echo "Running: ${CLI} run-scenario --config test/fixtures/scenarios/smoke-100.toml --out ${OUT_DIR}"
echo ""

set +e
(cd "${REPO_ROOT}" && "${CLI}" run-scenario \
  --config test/fixtures/scenarios/smoke-100.toml \
  --out "${OUT_DIR}" 2>&1)
RUN_EXIT=$?
set -e

assert_exit_code "${RUN_EXIT}" 0 "run-scenario exit code"

# ── Assert summary.json ───────────────────────────────────────────────────────
echo ""
echo "Asserting summary.json..."

assert_jq_eq  "${SUMMARY}" '.outcome'                            "succeeded"  "outcome == succeeded"
assert_jq_eq  "${SUMMARY}" '.subscribers.established'           "100"        "established == 100"
assert_jq_eq  "${SUMMARY}" '.subscribers.auth_failed'           "0"          "auth_failed == 0"
assert_jq_eq  "${SUMMARY}" '.subscribers.acct_failed'           "0"          "acct_failed == 0"
assert_jq_eq  "${SUMMARY}" '.subscribers.still_in_flight_at_end' "0"         "still_in_flight_at_end == 0"
assert_jq_lt  "${SUMMARY}" '.duration_ms'                        "30000"     "duration_ms < 30000"
assert_jq_ne  "${SUMMARY}" '.run_id'                             "null"      "run_id set"
assert_jq_ne  "${SUMMARY}" '.started_at'                         "null"      "started_at set"
assert_jq_ne  "${SUMMARY}" '.finished_at'                        "null"      "finished_at set"

# ── Assert artifact files ─────────────────────────────────────────────────────
echo ""
echo "Asserting artifact files..."

assert_file_exists "${OUT_DIR}/events.parquet"      "events.parquet exists and non-empty"
assert_file_exists "${OUT_DIR}/subscribers.parquet" "subscribers.parquet exists and non-empty"

# Optional: assert row count using parquet-row-count binary
assert_parquet_rows "${PARQUET_COUNT}" "${OUT_DIR}/subscribers.parquet" "100" \
  "subscribers.parquet has 100 rows"

# ── Summary ───────────────────────────────────────────────────────────────────
assert_summary "smoke-100"
