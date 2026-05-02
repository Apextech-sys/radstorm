#!/usr/bin/env bash
# E2E test orchestrator: brings up Docker FreeRADIUS, runs all scenarios, tears down.
#
# Purpose:
#   Top-level entry point for the radstorm end-to-end test suite. It:
#     1. Verifies prerequisites (Docker, Go, jq, curl, make)
#     2. Brings up the FreeRADIUS Docker rig and waits for health
#     3. Builds the parquet-row-count assertion utility if not present
#     4. Runs each scenario script under test/e2e/scenarios/ in order
#     5. Tears down Docker on exit (even on error)
#     6. Exits with the worst (highest) exit code from any scenario
#
#   Exit codes:
#     0   all scenarios passed
#     1   one or more scenarios failed assertions
#     2   a scenario was blocked (binary not built — parallel wave not merged yet)
#     3   prerequisite check failed (Docker/jq/curl not available)
#
# Related:
#   - test/e2e/assertions.sh (shared assertion helpers)
#   - test/e2e/scenarios/*.sh (scenario scripts)
#   - test/docker/docker-compose.yml (FreeRADIUS rig)
#   - Makefile (docker-up, build-cli, build-api targets)
#
# Briefing: .orchestration/briefings/3d-e2e.md
# Contract: stable exit code semantics above; all output goes to stdout/stderr
#           (no side effects on the repo beyond /tmp/radstorm-e2e/)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SCENARIOS_DIR="${SCRIPT_DIR}/scenarios"
COMPOSE_FILE="${REPO_ROOT}/test/docker/docker-compose.yml"
PARQUET_COUNT_SRC="${SCRIPT_DIR}/parquet-row-count/main.go"
PARQUET_COUNT_BIN="${SCRIPT_DIR}/parquet-row-count/parquet-row-count"

# ── Colour codes ──────────────────────────────────────────────────────────────
RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[0;33m'
CYAN=$'\033[0;36m'
BOLD=$'\033[1m'
RESET=$'\033[0m'
if [ ! -t 1 ]; then
  RED='' GREEN='' YELLOW='' CYAN='' BOLD='' RESET=''
fi

DOCKER_UP=false

# ── Docker teardown trap ──────────────────────────────────────────────────────
teardown() {
  local code=$?
  if [ "${DOCKER_UP}" = "true" ]; then
    echo ""
    echo "${CYAN}Tearing down Docker FreeRADIUS rig...${RESET}"
    docker compose -f "${COMPOSE_FILE}" down -v 2>&1 || true
    echo "Docker rig stopped."
  fi
  exit "${code}"
}
trap teardown EXIT
trap 'echo ""; echo "${RED}Interrupted.${RESET}"; exit 130' INT TERM

# ── Banner ────────────────────────────────────────────────────────────────────
echo ""
echo "${BOLD}${CYAN}╔══════════════════════════════════════════════════╗${RESET}"
echo "${BOLD}${CYAN}║   radstorm E2E Test Suite                        ║${RESET}"
echo "${BOLD}${CYAN}╚══════════════════════════════════════════════════╝${RESET}"
echo ""
echo "Repo root : ${REPO_ROOT}"
echo "Scenarios : ${SCENARIOS_DIR}"
echo "Started   : $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo ""

# ── Step 0: Prerequisite checks ───────────────────────────────────────────────
echo "${BOLD}--- Prerequisites ---${RESET}"
PREREQ_FAIL=0

check_tool() {
  local tool="$1"
  if command -v "${tool}" > /dev/null 2>&1; then
    echo "  ${GREEN}[OK]${RESET}  ${tool}"
  else
    echo "  ${RED}[MISS]${RESET} ${tool} — not found in PATH"
    (( PREREQ_FAIL++ )) || true
  fi
}

check_tool docker
# Compose v2 ships as `docker compose`; v1 was a standalone `docker-compose` binary.
# We check for either. `command -v` cannot handle multi-word subcommands, so test v2
# by running it directly.
if command -v docker-compose > /dev/null 2>&1; then
  echo "  ${GREEN}[OK]${RESET}  docker-compose (v1)"
elif docker compose version > /dev/null 2>&1; then
  echo "  ${GREEN}[OK]${RESET}  docker compose (v2)"
else
  echo "  ${RED}[MISS]${RESET} docker compose — neither docker-compose nor 'docker compose' available"
  (( PREREQ_FAIL++ )) || true
fi
check_tool go
check_tool jq
check_tool curl
check_tool make

if [ "${PREREQ_FAIL}" -gt 0 ]; then
  echo ""
  echo "${RED}[ABORT]${RESET} ${PREREQ_FAIL} prerequisite(s) missing. Cannot run E2E suite."
  exit 3
fi

echo ""

# ── Step 1: Build parquet-row-count utility ───────────────────────────────────
echo "${BOLD}--- Building parquet-row-count utility ---${RESET}"
if [ ! -x "${PARQUET_COUNT_BIN}" ]; then
  echo "Building from ${PARQUET_COUNT_SRC}..."
  (cd "${REPO_ROOT}" && go build -o "${PARQUET_COUNT_BIN}" ./test/e2e/parquet-row-count/ 2>&1) && \
    echo "  ${GREEN}[OK]${RESET} parquet-row-count built" || \
    echo "  ${YELLOW}[WARN]${RESET} parquet-row-count build failed — parquet row-count assertions will be skipped"
else
  echo "  ${GREEN}[OK]${RESET} parquet-row-count already built"
fi
echo ""

# ── Step 2: Bring up FreeRADIUS Docker rig ────────────────────────────────────
echo "${BOLD}--- Bringing up FreeRADIUS Docker rig ---${RESET}"
docker compose -f "${COMPOSE_FILE}" up -d 2>&1
DOCKER_UP=true
echo ""

echo "Waiting for FreeRADIUS to pass health check (up to 60s)..."
RADIUS_READY=false
for i in $(seq 1 60); do
  STATUS=$(docker inspect --format='{{.State.Health.Status}}' radstorm-freeradius 2>/dev/null || echo "unknown")
  if [ "${STATUS}" = "healthy" ]; then
    RADIUS_READY=true
    echo "  ${GREEN}[OK]${RESET} FreeRADIUS healthy after ${i}s"
    break
  fi
  sleep 1
done

if [ "${RADIUS_READY}" != "true" ]; then
  echo "  ${RED}[FAIL]${RESET} FreeRADIUS did not become healthy in 60s"
  echo "  Docker logs:"
  docker compose -f "${COMPOSE_FILE}" logs freeradius 2>&1 | tail -30 || true
  exit 1
fi
echo ""

# ── Step 3: Run scenarios ──────────────────────────────────────────────────────
SCENARIOS=(
  "smoke-100.sh"
  "cli-flows.sh"
  "api-flow.sh"
)

WORST_EXIT=0
declare -A SCENARIO_RESULTS

for scenario_file in "${SCENARIOS[@]}"; do
  scenario_path="${SCENARIOS_DIR}/${scenario_file}"
  scenario_name="${scenario_file%.sh}"

  if [ ! -f "${scenario_path}" ]; then
    echo "${RED}[ERROR]${RESET} scenario not found: ${scenario_path}"
    SCENARIO_RESULTS["${scenario_name}"]="MISSING"
    WORST_EXIT=1
    continue
  fi

  chmod +x "${scenario_path}"

  echo "${BOLD}--- Running: ${scenario_name} ---${RESET}"
  set +e
  bash "${scenario_path}"
  EXIT_CODE=$?
  set -e

  if [ "${EXIT_CODE}" -eq 0 ]; then
    SCENARIO_RESULTS["${scenario_name}"]="PASS"
    echo "${GREEN}[PASS]${RESET} ${scenario_name} (exit 0)"
  elif [ "${EXIT_CODE}" -eq 2 ]; then
    SCENARIO_RESULTS["${scenario_name}"]="BLOCKED"
    echo "${YELLOW}[BLOCKED]${RESET} ${scenario_name} — parallel wave binary not yet merged (exit 2)"
    echo "          Expected to pass after Wave 3A/3B merge in Wave 4."
  else
    SCENARIO_RESULTS["${scenario_name}"]="FAIL"
    echo "${RED}[FAIL]${RESET} ${scenario_name} (exit ${EXIT_CODE})"
  fi

  # Track worst exit (2 = blocked, 1 = fail; worst wins)
  if [ "${EXIT_CODE}" -gt "${WORST_EXIT}" ]; then
    WORST_EXIT="${EXIT_CODE}"
  fi
  echo ""
done

# ── Step 4: Final report ──────────────────────────────────────────────────────
echo "${BOLD}${CYAN}══════════════════════════════════════════════════${RESET}"
echo "${BOLD}  E2E Suite Summary${RESET}"
echo "${BOLD}${CYAN}══════════════════════════════════════════════════${RESET}"
for scenario_name in "${!SCENARIO_RESULTS[@]}"; do
  result="${SCENARIO_RESULTS[${scenario_name}]}"
  case "${result}" in
    PASS)    echo "  ${GREEN}PASS${RESET}     ${scenario_name}" ;;
    BLOCKED) echo "  ${YELLOW}BLOCKED${RESET}  ${scenario_name} — needs Wave 3A/3B merge" ;;
    FAIL)    echo "  ${RED}FAIL${RESET}     ${scenario_name}" ;;
    MISSING) echo "  ${RED}MISSING${RESET}  ${scenario_name}" ;;
    *)       echo "  UNKNOWN  ${scenario_name}" ;;
  esac
done
echo ""
echo "Finished: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo ""

if [ "${WORST_EXIT}" -eq 0 ]; then
  echo "${GREEN}${BOLD}All scenarios PASSED.${RESET}"
elif [ "${WORST_EXIT}" -eq 2 ]; then
  echo "${YELLOW}${BOLD}Some scenarios BLOCKED (CLI/API binaries not yet available).${RESET}"
  echo "Re-run after Wave 4 merge to get a full green result."
else
  echo "${RED}${BOLD}One or more scenarios FAILED.${RESET}"
fi
echo ""

exit "${WORST_EXIT}"
