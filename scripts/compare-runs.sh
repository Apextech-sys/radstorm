#!/usr/bin/env bash
# compare-runs.sh — compare two radstorm run directories and print a metric delta table.
#
# Purpose:
#   Reads summary.json from two run directories, extracts key metrics, and
#   prints a markdown comparison table showing the absolute values for each
#   candidate and the delta. Used during RADIUS server evaluation campaigns.
#
# Usage:
#   bash scripts/compare-runs.sh <run-dir-a> <run-dir-b>
#
# Arguments:
#   run-dir-a   Path to the first run's output directory (contains summary.json)
#   run-dir-b   Path to the second run's output directory (contains summary.json)
#
# Output:
#   Markdown table printed to stdout. Redirect to a file or paste into a report.
#
# Requirements:
#   jq (any modern version)
#
# Related: docs/COMPARISON-WORKFLOW.md, docs/EVALUATION-GUIDE.md §5
# Briefing: wave-6/handoff-docs

set -euo pipefail

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
if [ $# -lt 2 ]; then
  echo "Usage: $0 <run-dir-a> <run-dir-b>"
  echo
  echo "  run-dir-a  Path to first run output directory (contains summary.json)"
  echo "  run-dir-b  Path to second run output directory (contains summary.json)"
  echo
  echo "Example:"
  echo "  $0 /data/results/freeradius/coldstart-100k-run2 /data/results/interstellar/coldstart-100k-run2"
  exit 1
fi

DIR_A="$1"
DIR_B="$2"
SUMMARY_A="${DIR_A}/summary.json"
SUMMARY_B="${DIR_B}/summary.json"

# ---------------------------------------------------------------------------
# Validate inputs
# ---------------------------------------------------------------------------
if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq is required but not found in PATH" >&2
  exit 1
fi

if [ ! -f "${SUMMARY_A}" ]; then
  echo "error: summary.json not found in '${DIR_A}'" >&2
  exit 1
fi

if [ ! -f "${SUMMARY_B}" ]; then
  echo "error: summary.json not found in '${DIR_B}'" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Extract candidate names from directory names
# ---------------------------------------------------------------------------
NAME_A="$(basename "${DIR_A}")"
NAME_B="$(basename "${DIR_B}")"

# ---------------------------------------------------------------------------
# Helper: extract a field with a fallback for null/missing
# ---------------------------------------------------------------------------
field() {
  local file="$1"
  local path="$2"
  local fallback="${3:-N/A}"
  local val
  val="$(jq -r "${path} // \"${fallback}\"" "${file}" 2>/dev/null)"
  echo "${val}"
}

# ---------------------------------------------------------------------------
# Extract metrics
# ---------------------------------------------------------------------------
OUTCOME_A="$(field "${SUMMARY_A}" '.outcome')"
OUTCOME_B="$(field "${SUMMARY_B}" '.outcome')"

ESTABLISHED_A="$(field "${SUMMARY_A}" '.subscribers.established')"
ESTABLISHED_B="$(field "${SUMMARY_B}" '.subscribers.established')"

TOTAL_A="$(field "${SUMMARY_A}" '.subscribers.total')"
TOTAL_B="$(field "${SUMMARY_B}" '.subscribers.total')"

AUTH_FAILED_A="$(field "${SUMMARY_A}" '.subscribers.auth_failed')"
AUTH_FAILED_B="$(field "${SUMMARY_B}" '.subscribers.auth_failed')"

ACCT_FAILED_A="$(field "${SUMMARY_A}" '.subscribers.acct_failed')"
ACCT_FAILED_B="$(field "${SUMMARY_B}" '.subscribers.acct_failed')"

IN_FLIGHT_A="$(field "${SUMMARY_A}" '.subscribers.still_in_flight_at_end')"
IN_FLIGHT_B="$(field "${SUMMARY_B}" '.subscribers.still_in_flight_at_end')"

TIME_FULL_A="$(field "${SUMMARY_A}" '.establishment.time_to_full_ms')"
TIME_FULL_B="$(field "${SUMMARY_B}" '.establishment.time_to_full_ms')"

P50_A="$(field "${SUMMARY_A}" '.establishment.latency_ms.p50')"
P50_B="$(field "${SUMMARY_B}" '.establishment.latency_ms.p50')"

P95_A="$(field "${SUMMARY_A}" '.establishment.latency_ms.p95')"
P95_B="$(field "${SUMMARY_B}" '.establishment.latency_ms.p95')"

P99_A="$(field "${SUMMARY_A}" '.establishment.latency_ms.p99')"
P99_B="$(field "${SUMMARY_B}" '.establishment.latency_ms.p99')"

P999_A="$(field "${SUMMARY_A}" '.establishment.latency_ms.p999')"
P999_B="$(field "${SUMMARY_B}" '.establishment.latency_ms.p999')"

RETX_A="$(field "${SUMMARY_A}" '.retransmits.total')"
RETX_B="$(field "${SUMMARY_B}" '.retransmits.total')"

UNRESPONSIVE_A="$(jq '.server_health.unresponsive_periods | length' "${SUMMARY_A}" 2>/dev/null || echo 'N/A')"
UNRESPONSIVE_B="$(jq '.server_health.unresponsive_periods | length' "${SUMMARY_B}" 2>/dev/null || echo 'N/A')"

THRESHOLD_A="$(field "${SUMMARY_A}" '.thresholds.overall')"
THRESHOLD_B="$(field "${SUMMARY_B}" '.thresholds.overall')"

DURATION_A="$(field "${SUMMARY_A}" '.duration_ms')"
DURATION_B="$(field "${SUMMARY_B}" '.duration_ms')"

# ---------------------------------------------------------------------------
# Helper: compute numeric delta and determine winner
# ---------------------------------------------------------------------------
# delta <val_a> <val_b> <lower_is_better>
# Prints: delta | winner
# For non-numeric (N/A or strings): prints "—  | tie"
delta_and_winner() {
  local a="$1"
  local b="$2"
  local lower_is_better="${3:-true}"  # true = lower value wins (latency, failures)

  # Handle non-numeric values
  if ! [[ "${a}" =~ ^-?[0-9]+(\.[0-9]+)?$ ]] || ! [[ "${b}" =~ ^-?[0-9]+(\.[0-9]+)?$ ]]; then
    if [ "${a}" = "${b}" ]; then
      echo "— | tie"
    else
      echo "— | —"
    fi
    return
  fi

  local delta
  delta=$(( b - a ))

  local winner
  if [ "${a}" -eq "${b}" ]; then
    winner="tie"
  elif [ "${lower_is_better}" = "true" ]; then
    if [ "${a}" -lt "${b}" ]; then
      winner="${NAME_A}"
    else
      winner="${NAME_B}"
    fi
  else
    if [ "${a}" -gt "${b}" ]; then
      winner="${NAME_A}"
    else
      winner="${NAME_B}"
    fi
  fi

  if [ "${delta}" -ge 0 ]; then
    echo "+${delta} | ${winner}"
  else
    echo "${delta} | ${winner}"
  fi
}

# ---------------------------------------------------------------------------
# Print markdown table
# ---------------------------------------------------------------------------
echo "## Comparison: ${NAME_A} vs ${NAME_B}"
echo
echo "| Metric | ${NAME_A} | ${NAME_B} | Delta (B-A) | Better |"
echo "|---|---|---|---|---|"

# String metrics (no delta)
if [ "${OUTCOME_A}" = "${OUTCOME_B}" ]; then
  echo "| outcome | ${OUTCOME_A} | ${OUTCOME_B} | — | tie |"
else
  echo "| outcome | ${OUTCOME_A} | ${OUTCOME_B} | — | — |"
fi

# Established (higher is better)
read -r d w <<< "$(delta_and_winner "${ESTABLISHED_A}" "${ESTABLISHED_B}" false)"
echo "| established | ${ESTABLISHED_A}/${TOTAL_A} | ${ESTABLISHED_B}/${TOTAL_B} | ${d} | ${w} |"

# auth_failed (lower is better)
read -r d w <<< "$(delta_and_winner "${AUTH_FAILED_A}" "${AUTH_FAILED_B}" true)"
echo "| auth_failed | ${AUTH_FAILED_A} | ${AUTH_FAILED_B} | ${d} | ${w} |"

# acct_failed (lower is better)
read -r d w <<< "$(delta_and_winner "${ACCT_FAILED_A}" "${ACCT_FAILED_B}" true)"
echo "| acct_failed | ${ACCT_FAILED_A} | ${ACCT_FAILED_B} | ${d} | ${w} |"

# still_in_flight_at_end (lower is better)
read -r d w <<< "$(delta_and_winner "${IN_FLIGHT_A}" "${IN_FLIGHT_B}" true)"
echo "| still_in_flight_at_end | ${IN_FLIGHT_A} | ${IN_FLIGHT_B} | ${d} | ${w} |"

# time_to_full_ms (lower is better)
read -r d w <<< "$(delta_and_winner "${TIME_FULL_A}" "${TIME_FULL_B}" true)"
echo "| time_to_full_ms | ${TIME_FULL_A} | ${TIME_FULL_B} | ${d} | ${w} |"

# Latency (lower is better)
read -r d w <<< "$(delta_and_winner "${P50_A}" "${P50_B}" true)"
echo "| latency_p50_ms | ${P50_A} | ${P50_B} | ${d} | ${w} |"

read -r d w <<< "$(delta_and_winner "${P95_A}" "${P95_B}" true)"
echo "| latency_p95_ms | ${P95_A} | ${P95_B} | ${d} | ${w} |"

read -r d w <<< "$(delta_and_winner "${P99_A}" "${P99_B}" true)"
echo "| latency_p99_ms | ${P99_A} | ${P99_B} | ${d} | ${w} |"

read -r d w <<< "$(delta_and_winner "${P999_A}" "${P999_B}" true)"
echo "| latency_p999_ms | ${P999_A} | ${P999_B} | ${d} | ${w} |"

# Retransmits (lower is better)
read -r d w <<< "$(delta_and_winner "${RETX_A}" "${RETX_B}" true)"
echo "| retransmits_total | ${RETX_A} | ${RETX_B} | ${d} | ${w} |"

# Unresponsive periods (lower is better)
read -r d w <<< "$(delta_and_winner "${UNRESPONSIVE_A}" "${UNRESPONSIVE_B}" true)"
echo "| unresponsive_periods | ${UNRESPONSIVE_A} | ${UNRESPONSIVE_B} | ${d} | ${w} |"

# Duration (lower is better)
read -r d w <<< "$(delta_and_winner "${DURATION_A}" "${DURATION_B}" true)"
echo "| duration_ms | ${DURATION_A} | ${DURATION_B} | ${d} | ${w} |"

# Thresholds (string; same = tie, different = flag)
if [ "${THRESHOLD_A}" = "${THRESHOLD_B}" ]; then
  echo "| thresholds_overall | ${THRESHOLD_A} | ${THRESHOLD_B} | — | tie |"
else
  echo "| thresholds_overall | ${THRESHOLD_A} | ${THRESHOLD_B} | — | — |"
fi

echo
echo "_Delta = B minus A. Positive delta means B is higher. For latency and failure metrics, lower is better._"
echo "_Run directories: A=${DIR_A}  B=${DIR_B}_"
