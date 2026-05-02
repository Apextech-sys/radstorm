#!/usr/bin/env bash
# API flow scenario: starts the API server, drives a run via curl+SSE, asserts results.
#
# Purpose:
#   Verifies the full REST API surface end-to-end:
#     1. Build radstorm-api binary (make build-api)
#     2. Start API server on port 18080 (high port to avoid collisions)
#     3. Health probe → { "status": "ok" }
#     4. POST /api/v1/runs with smoke-100 config → 201 + run id
#     5. Stream SSE from /api/v1/runs/{id}/events for up to 90s, collect events
#     6. GET /api/v1/runs/{id}/results → summary.json with established_count == 100
#     7. Kill API server cleanly
#
# Uses 127.0.0.1 throughout (not localhost) to avoid IPv6 dualstack issues on Windows.
# The API server must have access to the running FreeRADIUS Docker rig (ports 11812/11813).
#
# Related:
#   - test/e2e/fixtures/api-create-run.json (POST body)
#   - .orchestration/contracts/rest-api.md (endpoint spec)
#   - .orchestration/contracts/results-schema.md (results shape)
#   - test/e2e/assertions.sh (helpers)
#
# Briefing: .orchestration/briefings/3d-e2e.md
# Contract: exits 0 on success, exit 2 if API binary missing, exit 1 on assertion failure

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
E2E_DIR="${REPO_ROOT}/test/e2e"
API_BIN="${REPO_ROOT}/bin/radstorm-api"
API_ADDR="127.0.0.1"
API_PORT="18080"
API_BASE="http://${API_ADDR}:${API_PORT}/api/v1"
CREATE_RUN_JSON="${E2E_DIR}/fixtures/api-create-run.json"
RESULTS_DIR="/tmp/radstorm-e2e/api-flow"
API_PID=""

# shellcheck source=test/e2e/assertions.sh
source "${E2E_DIR}/assertions.sh"

echo ""
echo "${CYAN}=== Scenario: api-flow ===${RESET}"
echo ""

assert_reset

# ── Cleanup trap — always kill API server ─────────────────────────────────────
cleanup_api() {
  if [ -n "${API_PID}" ] && kill -0 "${API_PID}" 2>/dev/null; then
    echo "Stopping API server (pid ${API_PID})..."
    kill "${API_PID}" 2>/dev/null || true
    wait "${API_PID}" 2>/dev/null || true
  fi
}
trap cleanup_api EXIT INT TERM

# ── Gate: API binary must exist ───────────────────────────────────────────────
if [ ! -f "${API_BIN}" ]; then
  echo "${RED}[BLOCKED]${RESET} API binary not found at ${API_BIN}"
  echo "  Expected after Wave 3B merge. Building now..."
  (cd "${REPO_ROOT}" && make build-api 2>&1) || {
    echo ""
    echo "${RED}[BLOCKED]${RESET} make build-api failed."
    echo "  This scenario requires Wave 3B (apps/api/) to be merged."
    echo "  Once orchestrator merges wave-3/3b-api into this worktree,"
    echo "  re-run: bash test/e2e/run.sh"
    exit 2
  }
fi

mkdir -p "${RESULTS_DIR}"

# ── Step 1: Start API server ──────────────────────────────────────────────────
echo "--- Step 1: Start API server on :${API_PORT} ---"
RADSTORM_API_ADDR=":${API_PORT}" \
RADSTORM_DATA_DIR="${RESULTS_DIR}/api-data" \
  "${API_BIN}" > "${RESULTS_DIR}/api-server.log" 2>&1 &
API_PID=$!
echo "API server started (pid ${API_PID})"

# Wait for API to be ready (up to 10s)
echo "Waiting for API to be ready..."
READY=false
for i in $(seq 1 20); do
  if curl -sf "http://${API_ADDR}:${API_PORT}/api/v1/health" > /dev/null 2>&1; then
    READY=true
    break
  fi
  sleep 0.5
done

if [ "${READY}" != "true" ]; then
  echo "${RED}[FAIL]${RESET} API server did not become ready in 10s"
  echo "API server log:"
  cat "${RESULTS_DIR}/api-server.log" 2>/dev/null || true
  exit 1
fi

echo "API server ready."

# ── Step 2: Health probe ──────────────────────────────────────────────────────
echo ""
echo "--- Step 2: GET /health ---"
HEALTH_RESP=$(curl -sf "${API_BASE}/health" 2>&1) || {
  echo "${RED}[FAIL]${RESET} health probe failed"
  (( ASSERT_FAIL++ )) || true
  HEALTH_RESP="{}"
}

HEALTH_TMP="${RESULTS_DIR}/health.json"
echo "${HEALTH_RESP}" > "${HEALTH_TMP}"
assert_jq_eq "${HEALTH_TMP}" '.status' "ok" "health.status == ok"

# ── Step 3: POST /runs ────────────────────────────────────────────────────────
echo ""
echo "--- Step 3: POST /api/v1/runs ---"
set +e
CREATE_RESP=$(curl -sf -w '\n%{http_code}' \
  -X POST "${API_BASE}/runs" \
  -H "Content-Type: application/json" \
  -d "@${CREATE_RUN_JSON}" 2>&1)
CURL_EXIT=$?
set -e

if [ ${CURL_EXIT} -ne 0 ]; then
  echo "${RED}[FAIL]${RESET} POST /runs curl failed (exit ${CURL_EXIT})"
  (( ASSERT_FAIL++ )) || true
else
  HTTP_CODE=$(echo "${CREATE_RESP}" | tail -1)
  CREATE_BODY=$(echo "${CREATE_RESP}" | head -n -1)

  CREATE_TMP="${RESULTS_DIR}/create-run.json"
  echo "${CREATE_BODY}" > "${CREATE_TMP}"

  assert_exit_code "${HTTP_CODE}" "201" "POST /runs HTTP status 201"
  assert_jq_ne "${CREATE_TMP}" '.id' "null" "run id returned"
  assert_jq_ne "${CREATE_TMP}" '.status' "null" "run status returned"

  RUN_ID=$(jq -r '.id' "${CREATE_TMP}" 2>/dev/null || echo "")
fi

# ── Step 4: Stream SSE events ─────────────────────────────────────────────────
echo ""
echo "--- Step 4: Stream SSE /api/v1/runs/${RUN_ID}/events ---"
SSE_OUT="${RESULTS_DIR}/sse-events.txt"

if [ -z "${RUN_ID:-}" ]; then
  echo "${YELLOW}[SKIP]${RESET} Cannot stream SSE — no run id (prior step failed)"
else
  # Stream SSE for up to 90 seconds or until we see a 'complete' event
  set +e
  timeout 90 bash -c "
    curl -sf -N -H 'Accept: text/event-stream' \
      '${API_BASE}/runs/${RUN_ID}/events' \
    | tee '${SSE_OUT}' \
    | grep -m 1 'event: complete' > /dev/null 2>&1
  " || true
  set -e

  if [ -s "${SSE_OUT}" ]; then
    echo "${GREEN}[PASS]${RESET} received SSE events"
    (( ASSERT_PASS++ )) || true
    # Check at least one progress event was received
    if grep -q "event: progress" "${SSE_OUT}"; then
      echo "${GREEN}[PASS]${RESET} received progress SSE events"
      (( ASSERT_PASS++ )) || true
    else
      echo "${YELLOW}[WARN]${RESET} no progress events in SSE stream (may be timing)"
    fi
  else
    echo "${RED}[FAIL]${RESET} no SSE events received"
    (( ASSERT_FAIL++ )) || true
  fi
fi

# ── Step 5: Wait for run to complete ─────────────────────────────────────────
echo ""
echo "--- Step 5: Wait for run to complete ---"
if [ -z "${RUN_ID:-}" ]; then
  echo "${YELLOW}[SKIP]${RESET} No run id"
else
  COMPLETE=false
  for i in $(seq 1 90); do
    RUN_STATUS_TMP="${RESULTS_DIR}/run-status.json"
    set +e
    curl -sf "${API_BASE}/runs/${RUN_ID}" > "${RUN_STATUS_TMP}" 2>/dev/null
    set -e
    if [ -f "${RUN_STATUS_TMP}" ]; then
      STATUS=$(jq -r '.status' "${RUN_STATUS_TMP}" 2>/dev/null || echo "unknown")
      if [ "${STATUS}" = "succeeded" ] || [ "${STATUS}" = "failed" ] || [ "${STATUS}" = "cancelled" ]; then
        echo "Run reached terminal status: ${STATUS}"
        COMPLETE=true
        break
      fi
    fi
    sleep 1
  done

  if [ "${COMPLETE}" = "true" ]; then
    echo "${GREEN}[PASS]${RESET} run reached terminal status"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} run did not complete within 90s"
    (( ASSERT_FAIL++ )) || true
  fi
fi

# ── Step 6: Assert results ────────────────────────────────────────────────────
echo ""
echo "--- Step 6: GET /api/v1/runs/${RUN_ID}/results ---"
if [ -z "${RUN_ID:-}" ]; then
  echo "${YELLOW}[SKIP]${RESET} No run id"
else
  RESULTS_TMP="${RESULTS_DIR}/results.json"
  set +e
  curl -sf "${API_BASE}/runs/${RUN_ID}/results" > "${RESULTS_TMP}" 2>/dev/null
  RESULTS_CURL_EXIT=$?
  set -e

  if [ ${RESULTS_CURL_EXIT} -ne 0 ] || [ ! -s "${RESULTS_TMP}" ]; then
    echo "${RED}[FAIL]${RESET} GET /results failed"
    (( ASSERT_FAIL++ )) || true
  else
    assert_jq_eq  "${RESULTS_TMP}" '.outcome'                  "succeeded" "results.outcome == succeeded"
    assert_jq_eq  "${RESULTS_TMP}" '.subscribers.established'  "100"       "results.established == 100"
    assert_jq_eq  "${RESULTS_TMP}" '.subscribers.auth_failed'  "0"         "results.auth_failed == 0"
    assert_jq_lt  "${RESULTS_TMP}" '.duration_ms'              "30000"     "results.duration_ms < 30000"
  fi
fi

# ── Cleanup happens via trap ──────────────────────────────────────────────────

# ── Summary ───────────────────────────────────────────────────────────────────
assert_summary "api-flow"
