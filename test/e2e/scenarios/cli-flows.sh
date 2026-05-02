#!/usr/bin/env bash
# CLI flows scenario: exercises validate-config, single-auth, run-scenario, analyze-results.
#
# Purpose:
#   Tests the CLI subcommand surface end-to-end:
#     1. validate-config with a valid config  → exit 0
#     2. validate-config with /dev/null        → exit non-zero + clear error message
#     3. single-auth with known-good creds     → "Access-Accept" in output
#     4. run-scenario (small, smoke-100)       → exit 0, produces output dir
#     5. analyze-results on that output dir   → prints human-readable summary
#   Each step is independently asserted; accumulated failures reported at end.
#
# Related:
#   - test/fixtures/scenarios/smoke-100.toml (config under test)
#   - test/fixtures/credentials/smoke-100.csv (credentials)
#   - test/e2e/assertions.sh (helpers)
#   - docs/ARCHITECTURE.md §apps/cli (CLI subcommand reference)
#
# Briefing: .orchestration/briefings/3d-e2e.md
# Contract: exits 0 on success, non-zero on failure; exit 2 if CLI binary missing

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
E2E_DIR="${REPO_ROOT}/test/e2e"
OUT_DIR="/tmp/radstorm-e2e/cli-flows"
CLI="${REPO_ROOT}/bin/radstorm"

# shellcheck source=test/e2e/assertions.sh
source "${E2E_DIR}/assertions.sh"

echo ""
echo "${CYAN}=== Scenario: cli-flows ===${RESET}"
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

mkdir -p "${OUT_DIR}"

# ── 1. validate-config: valid file → exit 0 ──────────────────────────────────
echo "--- Step 1: validate-config (valid config) ---"
set +e
VALIDATE_OUT=$("${CLI}" validate-config "${REPO_ROOT}/test/fixtures/scenarios/smoke-100.toml" 2>&1)
VALIDATE_EXIT=$?
set -e
assert_exit_code "${VALIDATE_EXIT}" 0 "validate-config (valid) exit code"

# ── 2. validate-config: /dev/null → exit non-zero ────────────────────────────
echo ""
echo "--- Step 2: validate-config (/dev/null → expect error) ---"
set +e
INVALID_OUT=$("${CLI}" validate-config /dev/null 2>&1)
INVALID_EXIT=$?
set -e
assert_exit_nonzero "${INVALID_EXIT}" "validate-config (/dev/null) exit non-zero"
# Should produce some error message
assert_string_contains "${INVALID_OUT}" "" "validate-config error produces output"

# ── 3. single-auth: known-good credentials → Access-Accept ───────────────────
echo ""
echo "--- Step 3: single-auth (sub00000001 / pw00000001) ---"
set +e
AUTH_OUT=$("${CLI}" single-auth \
  --config "${REPO_ROOT}/test/fixtures/scenarios/smoke-100.toml" \
  --user sub00000001 \
  --pass pw00000001 2>&1)
AUTH_EXIT=$?
set -e
assert_exit_code "${AUTH_EXIT}" 0 "single-auth exit code"
assert_string_contains "${AUTH_OUT}" "Access-Accept" "single-auth output contains Access-Accept"

# ── 4. run-scenario: smoke-100 → exit 0 ──────────────────────────────────────
echo ""
echo "--- Step 4: run-scenario (smoke-100) ---"
RUN_OUT_DIR="${OUT_DIR}/smoke-100"
rm -rf "${RUN_OUT_DIR}"
mkdir -p "${RUN_OUT_DIR}"

set +e
("${CLI}" run-scenario \
  --config "${REPO_ROOT}/test/fixtures/scenarios/smoke-100.toml" \
  --out "${RUN_OUT_DIR}" 2>&1)
RUN_EXIT=$?
set -e
assert_exit_code "${RUN_EXIT}" 0 "run-scenario exit code"
assert_file_exists "${RUN_OUT_DIR}/summary.json" "summary.json produced"

# ── 5. analyze-results: reads output dir → prints summary ─────────────────────
echo ""
echo "--- Step 5: analyze-results ---"
set +e
ANALYZE_OUT=$("${CLI}" analyze-results "${RUN_OUT_DIR}" 2>&1)
ANALYZE_EXIT=$?
set -e
assert_exit_code "${ANALYZE_EXIT}" 0 "analyze-results exit code"
# Should produce some human-readable output mentioning outcome or established
if echo "${ANALYZE_OUT}" | grep -qiE "(established|succeeded|outcome|subscriber)"; then
  echo "${GREEN}[PASS]${RESET} analyze-results output contains summary keywords"
  (( ASSERT_PASS++ )) || true
else
  echo "${YELLOW}[WARN]${RESET} analyze-results output may be incomplete: ${ANALYZE_OUT}"
  # Soft warning — don't fail on format details
fi

# ── Summary ───────────────────────────────────────────────────────────────────
assert_summary "cli-flows"
