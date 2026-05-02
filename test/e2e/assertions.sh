#!/usr/bin/env bash
# jq-based JSON assertion helpers for the radstorm E2E test suite.
#
# Purpose:
#   Provides reusable bash functions that assert JSON field values using jq.
#   Each function prints a PASS/FAIL line with field name, expected, and actual
#   values, and returns exit code 0 on success or 1 on failure. Callers
#   accumulate failures and report at end of scenario.
#
# Related:
#   - test/e2e/run.sh (sources this file)
#   - test/e2e/scenarios/*.sh (use these helpers)
#   - .orchestration/contracts/results-schema.md (defines the JSON shape)
#
# Briefing: .orchestration/briefings/3d-e2e.md
# Contract: internal — helpers only; not consumed by production code

set -euo pipefail

# ── Colour codes (safe fallback if no terminal) ───────────────────────────────
RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[0;33m'
CYAN=$'\033[0;36m'
RESET=$'\033[0m'
if [ ! -t 1 ]; then
  RED='' GREEN='' YELLOW='' CYAN='' RESET=''
fi

# ── Internal counters (per-script) ────────────────────────────────────────────
ASSERT_PASS=0
ASSERT_FAIL=0

# Reset counters — call at the top of each scenario
assert_reset() {
  ASSERT_PASS=0
  ASSERT_FAIL=0
}

# Print summary and return 0 if all passed, 1 otherwise
assert_summary() {
  local label="${1:-Assertions}"
  echo ""
  if [ "${ASSERT_FAIL}" -eq 0 ]; then
    echo "${GREEN}[PASS]${RESET} ${label}: ${ASSERT_PASS} passed, 0 failed"
    return 0
  else
    echo "${RED}[FAIL]${RESET} ${label}: ${ASSERT_PASS} passed, ${ASSERT_FAIL} failed"
    return 1
  fi
}

# ── Core assertion primitives ─────────────────────────────────────────────────

# assert_jq_eq <file> <jq_expr> <expected_value> [<label>]
# Evaluates a jq expression on a JSON file and asserts the output == expected.
assert_jq_eq() {
  local file="$1"
  local expr="$2"
  local expected="$3"
  local label="${4:-${expr}}"

  if [ ! -f "${file}" ]; then
    echo "${RED}[FAIL]${RESET} ${label}: file not found: ${file}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi

  local actual
  actual=$(jq -r "${expr}" "${file}" 2>&1) || {
    echo "${RED}[FAIL]${RESET} ${label}: jq error: ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  }

  if [ "${actual}" = "${expected}" ]; then
    echo "${GREEN}[PASS]${RESET} ${label}: ${actual} == ${expected}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected=${expected} actual=${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_jq_ne <file> <jq_expr> <unexpected_value> [<label>]
# Asserts the jq output is NOT equal to a value (e.g. null, 0).
assert_jq_ne() {
  local file="$1"
  local expr="$2"
  local unexpected="$3"
  local label="${4:-${expr} != ${unexpected}}"

  if [ ! -f "${file}" ]; then
    echo "${RED}[FAIL]${RESET} ${label}: file not found: ${file}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi

  local actual
  actual=$(jq -r "${expr}" "${file}" 2>&1) || {
    echo "${RED}[FAIL]${RESET} ${label}: jq error: ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  }

  if [ "${actual}" != "${unexpected}" ]; then
    echo "${GREEN}[PASS]${RESET} ${label}: ${actual} != ${unexpected}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected != ${unexpected} but got ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_jq_lt <file> <jq_expr> <threshold> [<label>]
# Asserts the numeric jq output is strictly less than threshold.
assert_jq_lt() {
  local file="$1"
  local expr="$2"
  local threshold="$3"
  local label="${4:-${expr} < ${threshold}}"

  if [ ! -f "${file}" ]; then
    echo "${RED}[FAIL]${RESET} ${label}: file not found: ${file}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi

  local actual
  actual=$(jq -r "${expr}" "${file}" 2>&1) || {
    echo "${RED}[FAIL]${RESET} ${label}: jq error: ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  }

  if awk "BEGIN { exit (${actual} < ${threshold}) ? 0 : 1 }"; then
    echo "${GREEN}[PASS]${RESET} ${label}: ${actual} < ${threshold}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected < ${threshold} but got ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_jq_ge <file> <jq_expr> <threshold> [<label>]
# Asserts the numeric jq output is >= threshold.
assert_jq_ge() {
  local file="$1"
  local expr="$2"
  local threshold="$3"
  local label="${4:-${expr} >= ${threshold}}"

  if [ ! -f "${file}" ]; then
    echo "${RED}[FAIL]${RESET} ${label}: file not found: ${file}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi

  local actual
  actual=$(jq -r "${expr}" "${file}" 2>&1) || {
    echo "${RED}[FAIL]${RESET} ${label}: jq error: ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  }

  if awk "BEGIN { exit (${actual} >= ${threshold}) ? 0 : 1 }"; then
    echo "${GREEN}[PASS]${RESET} ${label}: ${actual} >= ${threshold}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected >= ${threshold} but got ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_file_exists <path> [<label>]
# Asserts that a file exists and is non-empty.
assert_file_exists() {
  local path="$1"
  local label="${2:-file exists: ${path}}"

  if [ -s "${path}" ]; then
    echo "${GREEN}[PASS]${RESET} ${label}"
    (( ASSERT_PASS++ )) || true
  elif [ -f "${path}" ]; then
    echo "${RED}[FAIL]${RESET} ${label}: file exists but is empty"
    (( ASSERT_FAIL++ )) || true
    return 1
  else
    echo "${RED}[FAIL]${RESET} ${label}: file not found"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_exit_code <actual_code> <expected_code> [<label>]
# Asserts that a previously captured exit code matches expected.
assert_exit_code() {
  local actual="$1"
  local expected="$2"
  local label="${3:-exit code}"

  if [ "${actual}" -eq "${expected}" ]; then
    echo "${GREEN}[PASS]${RESET} ${label}: exit code ${actual} == ${expected}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected exit ${expected} got ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_exit_nonzero <actual_code> [<label>]
# Asserts that a previously captured exit code is non-zero.
assert_exit_nonzero() {
  local actual="$1"
  local label="${2:-exit code non-zero}"

  if [ "${actual}" -ne 0 ]; then
    echo "${GREEN}[PASS]${RESET} ${label}: exit code ${actual} (non-zero as expected)"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected non-zero exit, got 0"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_string_contains <haystack_var> <needle> [<label>]
# Asserts that a string variable contains a substring.
assert_string_contains() {
  local haystack="$1"
  local needle="$2"
  local label="${3:-output contains '${needle}'}"

  if [[ "${haystack}" == *"${needle}"* ]]; then
    echo "${GREEN}[PASS]${RESET} ${label}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: '${needle}' not found in output"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}

# assert_parquet_rows <parquet_binary> <file> <expected_rows> [<label>]
# Uses the parquet-row-count util to assert row count in a Parquet file.
# <parquet_binary> is the path to test/e2e/parquet-row-count/parquet-row-count binary.
assert_parquet_rows() {
  local binary="$1"
  local file="$2"
  local expected="$3"
  local label="${4:-parquet rows in ${file}}"

  if [ ! -x "${binary}" ]; then
    echo "${YELLOW}[SKIP]${RESET} ${label}: parquet-row-count binary not found at ${binary}"
    return 0  # soft skip — not a hard failure; binary may not be built yet
  fi

  if [ ! -f "${file}" ]; then
    echo "${RED}[FAIL]${RESET} ${label}: parquet file not found: ${file}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi

  local actual
  actual=$("${binary}" "${file}" 2>&1) || {
    echo "${RED}[FAIL]${RESET} ${label}: parquet-row-count error: ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  }

  if [ "${actual}" = "${expected}" ]; then
    echo "${GREEN}[PASS]${RESET} ${label}: ${actual} rows == ${expected}"
    (( ASSERT_PASS++ )) || true
  else
    echo "${RED}[FAIL]${RESET} ${label}: expected ${expected} rows, got ${actual}"
    (( ASSERT_FAIL++ )) || true
    return 1
  fi
}
