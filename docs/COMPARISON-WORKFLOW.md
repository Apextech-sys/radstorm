<!-- Purpose: Workflow for running test campaigns against two RADIUS server candidates and producing a comparison report. -->

# Comparison Workflow

Audience: operator running a formal evaluation comparing two RADIUS server candidates. This document covers naming conventions, running matched campaigns, and producing stakeholder-ready comparison output.

---

## Table of contents

1. [Naming conventions](#1-naming-conventions)
2. [Running matched campaigns](#2-running-matched-campaigns)
3. [Producing a comparison](#3-producing-a-comparison)
4. [Stakeholder presentation format](#4-stakeholder-presentation-format)

---

## 1. Naming conventions

Keep result directories cleanly separated so you can run `scripts/compare-runs.sh` on any matched pair without guesswork.

### Directory structure

```
/data/results/
  <candidate-a>/
    <scenario>-<scale>-run1/   ← warm-up, discard
    <scenario>-<scale>-run2/   ← measurement run (use this one)
  <candidate-b>/
    <scenario>-<scale>-run1/
    <scenario>-<scale>-run2/
```

### Example

```
/data/results/
  freeradius-3.2/
    coldstart-1k-run1/
    coldstart-1k-run2/
    coldstart-10k-run2/
    coldstart-100k-run2/
    coldstart-1M-run2/
    pessimal-10k-run2/
    coa-100k-run2/
  interstellar-2.1/
    coldstart-1k-run1/
    coldstart-1k-run2/
    coldstart-10k-run2/
    coldstart-100k-run2/
    coldstart-1M-run2/
    pessimal-10k-run2/
    coa-100k-run2/
```

Include the software version in the candidate directory name. This makes it unambiguous which version produced each result, especially important if you re-run after a server software update.

---

## 2. Running matched campaigns

The scenario TOML must be identical between candidates except for the `[target]` block. Keep one base config per scenario and create per-candidate variants:

```bash
# Start from a base config
cp /data/scenarios/coldstart-100k-base.toml /data/scenarios/coldstart-100k-freeradius.toml
cp /data/scenarios/coldstart-100k-base.toml /data/scenarios/coldstart-100k-interstellar.toml

# Edit only the [target] block in each variant
vi /data/scenarios/coldstart-100k-freeradius.toml
vi /data/scenarios/coldstart-100k-interstellar.toml

# Verify they differ only in [target]
diff /data/scenarios/coldstart-100k-freeradius.toml \
     /data/scenarios/coldstart-100k-interstellar.toml
```

The diff should show only `auth_address`, `acct_address`, and `shared_secret` differences.

### Running one candidate

```bash
CANDIDATE=freeradius-3.2
SCENARIO=coldstart-100k

# Warm-up run (results not used for comparison)
./bin/radstorm run-scenario \
  --config /data/scenarios/${SCENARIO}-${CANDIDATE%%"-"*}.toml \
  --out /data/results/${CANDIDATE}/${SCENARIO}-run1

# Measurement run
./bin/radstorm run-scenario \
  --config /data/scenarios/${SCENARIO}-${CANDIDATE%%"-"*}.toml \
  --out /data/results/${CANDIDATE}/${SCENARIO}-run2 \
  --drain-seconds 60

# Quick check
jq '{outcome, established: .subscribers.established, total: .subscribers.total, p99: .establishment.latency_ms.p99, thresholds: .thresholds.overall}' \
  /data/results/${CANDIDATE}/${SCENARIO}-run2/summary.json
```

### Running both candidates for a full scenario matrix

```bash
#!/usr/bin/env bash
# Run the full evaluation matrix for both candidates.
# Adjust CANDIDATES, SCENARIOS, and paths for your environment.

CANDIDATES=("freeradius-3.2" "interstellar-2.1")
SCENARIOS=("coldstart-1k" "coldstart-10k" "coldstart-100k" "pessimal-10k")
RESULTS_DIR=/data/results

for CANDIDATE in "${CANDIDATES[@]}"; do
  for SCENARIO in "${SCENARIOS[@]}"; do
    CANDIDATE_SHORT="${CANDIDATE%%-*}"
    CONFIG="/data/scenarios/${SCENARIO}-${CANDIDATE_SHORT}.toml"
    OUT="${RESULTS_DIR}/${CANDIDATE}/${SCENARIO}-run2"

    echo "=== Running ${CANDIDATE} / ${SCENARIO} ==="

    # Validate before running
    ./bin/radstorm validate-config "${CONFIG}" || { echo "FAIL: validate-config"; continue; }

    ./bin/radstorm run-scenario \
      --config "${CONFIG}" \
      --out "${OUT}" \
      --drain-seconds 60

    echo "Result: $(jq -r '.thresholds.overall' "${OUT}/summary.json")"
    echo
  done
done
```

---

## 3. Producing a comparison

### Using compare-runs.sh

```bash
bash scripts/compare-runs.sh \
  /data/results/freeradius-3.2/coldstart-100k-run2 \
  /data/results/interstellar-2.1/coldstart-100k-run2
```

Output (markdown table ready to paste):

```
## Comparison: freeradius-3.2 vs interstellar-2.1
### Scenario: coldstart-100k-run2

| Metric | freeradius-3.2 | interstellar-2.1 | Delta | Winner |
|---|---|---|---|---|
| outcome | succeeded | succeeded | — | tie |
| established | 100000 | 100000 | 0 | tie |
| auth_failed | 0 | 0 | 0 | tie |
| time_to_full_ms | 378200 | 312450 | -65750 | interstellar-2.1 |
| latency p50 ms | 145 | 112 | -33 | interstellar-2.1 |
| latency p99 ms | 2300 | 1890 | -410 | interstellar-2.1 |
| latency p999 ms | 4100 | 3200 | -900 | interstellar-2.1 |
| retransmits total | 27 | 8 | -19 | interstellar-2.1 |
| unresponsive periods | 0 | 0 | 0 | tie |
| thresholds overall | pass | pass | — | tie |
```

### Running comparisons for all scenarios

```bash
#!/usr/bin/env bash
SCENARIOS=("coldstart-1k" "coldstart-10k" "coldstart-100k" "pessimal-10k")
A=freeradius-3.2
B=interstellar-2.1
RESULTS_DIR=/data/results

for SCENARIO in "${SCENARIOS[@]}"; do
  echo "---"
  bash scripts/compare-runs.sh \
    "${RESULTS_DIR}/${A}/${SCENARIO}-run2" \
    "${RESULTS_DIR}/${B}/${SCENARIO}-run2"
  echo
done
```

Redirect the output to a file for your report:

```bash
bash run-all-comparisons.sh > /data/comparison-report-$(date +%Y%m%d).md
```

---

## 4. Stakeholder presentation format

Combine all comparison tables into a single document with this structure:

```markdown
# RADIUS Server Evaluation — [Date]

## Executive summary

- **Candidate A:** FreeRADIUS 3.2.x on [hardware spec]
- **Candidate B:** Interstellar 2.1.x on [hardware spec]
- **Scenario:** ISP cold-start reconnect storm, Gaussian ramp
- **Scale tested:** 1k, 10k, 100k subscribers (1M pending dedicated hardware)
- **Recommendation:** [Your recommendation with one-line rationale]

## Test environment

| Parameter | Value |
|---|---|
| Test host | [hostname, CPU, RAM, NIC] |
| Test date | [date] |
| FreeRADIUS version | 3.2.x |
| Interstellar version | 2.1.x |
| RADIUS server hardware | [spec] |
| Credentials | [count] pre-seeded users in [DB backend] |

## Results by scenario

### 1k cold_start

[paste compare-runs.sh output]

### 10k cold_start

[paste compare-runs.sh output]

### 100k cold_start

[paste compare-runs.sh output]

### 10k pessimal

[paste compare-runs.sh output]

## Threshold pass/fail matrix

| Scenario | FreeRADIUS | Interstellar |
|---|---|---|
| 1k cold_start | pass | pass |
| 10k cold_start | pass | pass |
| 100k cold_start | pass | pass |
| 10k pessimal | pass | pass |

## Decision

[State which candidate wins on the primary metrics (time_to_full_ms, p99 latency, auth_failed) 
and any qualifications (e.g., "Interstellar wins on latency but has not been tested at 1M scale").]

## Reproduction instructions

To reproduce any run:

1. Follow docs/PRODUCTION-DEPLOYMENT.md to set up the test host
2. Install the candidate RADIUS server using configuration in `/data/server-configs/`
3. Run: `./bin/radstorm run-scenario --config /data/scenarios/<scenario>.toml --out /data/results/<candidate>/<scenario>-run2`
4. Config files and credentials are archived at: [path or storage location]
```

Keep the actual `summary.json` files, `config.toml` snapshots, and `run.log` for each run in a shared location accessible to the team. The markdown report is the human-readable summary; the JSON files are the audit trail.
