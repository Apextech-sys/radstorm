<!-- Purpose: End-to-end evaluation workflow for comparing RADIUS server candidates at ISP scale. -->

# radstorm Evaluation Guide

Audience: network engineering team running a formal evaluation of FreeRADIUS vs Interstellar (or any two RADIUS server candidates) at 1M-subscriber scale. This document tells you exactly what to do from a blank Linux box to a defensible comparison report.

---

## Table of contents

1. [Goal of the evaluation](#1-goal)
2. [Pre-evaluation prep](#2-pre-evaluation-prep)
3. [The test plan](#3-the-test-plan)
4. [Per-scenario procedure](#4-per-scenario-procedure)
5. [Comparing two candidates](#5-comparing-two-candidates)
6. [The decision matrix](#6-the-decision-matrix)
7. [Acceptance criteria](#7-acceptance-criteria)
8. [Reporting](#8-reporting)

---

## 1. Goal

**The question you are answering:** Which RADIUS server can handle a 1M-subscriber PPPoE reconnect storm — the kind that follows a BNG reboot or a core link failure — within acceptable latency and error bounds, sustained across multiple runs?

You are NOT benchmarking throughput in isolation. You are simulating a realistic operational scenario: a population of real-sized subscribers all re-authenticating within a narrow time window, with a real retransmit policy, real Huawei VSA payloads, and a real CoA listener on the test host. The results must be reproducible enough that a second engineer can run the same config and get the same order-of-magnitude numbers.

---

## 2. Pre-evaluation prep

### 2.1 Dedicated Linux box

Do not run the evaluation on a shared host or a virtual machine that shares CPU with other workloads. Latency measurements will be meaningless.

Minimum recommended hardware per scale tier:

| Scale | Subscribers | CPU cores | RAM | NIC | Disk (for Parquet) |
|---|---|---|---|---|---|
| Warm-up | 1k–10k | 4 | 8 GB | 1 GbE | 10 GB |
| Mid-scale | 100k | 8 | 16 GB | 10 GbE | 50 GB |
| Full scale | 1M | 16+ | 64 GB | 10 GbE | 500 GB |

The 1M-subscriber target is the **architectural target** of radstorm. This guide documents the procedure; the actual 1M run must be verified on hardware meeting the full-scale spec before any conclusions are drawn. Numbers labeled "expected" in this document are extrapolated from the 100-subscriber validated run and the architecture's design assumptions.

### 2.2 OS setup

Ubuntu 22.04 LTS or RHEL 9. Both are supported; examples use Ubuntu. See [docs/PRODUCTION-DEPLOYMENT.md](PRODUCTION-DEPLOYMENT.md) for the full production setup procedure including sysctl tuning, source IP aliases, and file descriptor limits. Complete that entire document before proceeding here.

### 2.3 Source IP aliases

You need enough source IP aliases to cover the concurrency you are testing. Capacity formula:

```
max_concurrent_inflight = num_src_ips × ports_per_ip × 256
```

For 1M subscribers at realistic ISP concurrency (~10% in-flight simultaneously):

```
100,000 in-flight ÷ 256 IDs = ~391 (srcIP, srcPort) pairs needed
With port_range [10000, 60000] (50,000 ports), 1 IP gives 50,000 × 256 = 12.8M capacity
In practice: 4 IPs with a 10k-port range each is a safe floor; 8 IPs is comfortable
```

For the pessimal scenario at 1M (all in-flight simultaneously):

```
1,000,000 ÷ 256 ≈ 3,907 pairs → 8 IPs × ~500 ports each = 8 × 500 × 256 = 1,024,000 ✓
```

Provision source IP aliases before running any scenario at a new scale tier. The test will fail fast with `ErrIdentifierExhausted` in `run.log` if you are under-provisioned; that is not a RADIUS server failure — it is a test harness configuration error.

```bash
# Add 8 aliases on interface eth0 (adjust interface name for your host)
for i in $(seq 1 8); do
    sudo ip addr add 192.168.100.${i}/24 dev eth0 label eth0:${i}
done

# Verify all are bound
ip addr show eth0 | grep 192.168.100

# Update your scenario config's [source] block with all 8 IPs
```

### 2.4 Candidate RADIUS server setup

The RADIUS server under test must be configured in production-equivalent mode:
- Same user database backend (SQL, LDAP, flat file with realistic row count)
- Same thread/process count as production
- Same policy rules (no "test mode" shortcuts)
- Same VSA dictionaries loaded (especially Huawei if your BNGs use them)
- Dedicated hardware or VM with resources matching your planned production deployment
- Shared secret configured identically to what you will put in `[target].shared_secret`

The RADIUS server and the radstorm test box should be on the same LAN or a low-latency switch fabric. Do not test across a WAN link — you will be measuring link RTT, not server performance.

**Document the candidate configuration** before each test run. Include software version, relevant config file excerpts, database row counts, and hardware. This becomes part of your evaluation report.

### 2.5 Credentials file

Generate a credential file that matches your subscriber count. Each row corresponds to one simulated PPPoE subscriber.

```bash
# Generate 1M credentials (from repo root)
make gen-credentials COUNT=1000000 OUT=/data/creds-1M.csv

# Verify row count
wc -l /data/creds-1M.csv
# Expected: 1000001 (header + 1M rows)
```

These credentials must be pre-loaded into the RADIUS server's user database before running. For FreeRADIUS with a SQL backend:

```bash
# Example: load into MySQL (adjust for your schema)
mysql -u radius -p radius < /data/radius-users-1M.sql
```

---

## 3. The test plan

Run scenarios in this order. Each tier validates the previous and surfaces issues cheaply.

| Step | Scenario | Count | Purpose |
|---|---|---|---|
| 1 | `uniform` | 1k | Baseline connectivity and config validation |
| 2 | `cold_start` | 1k | Verify Gaussian ramp works end-to-end |
| 3 | `cold_start` | 10k | First meaningful latency sample |
| 4 | `pessimal` | 10k | Burst stress — surfaces lock contention early |
| 5 | `cold_start` | 100k | Mid-scale validation — representative of a real BNG population |
| 6 | `coa_storm` | 100k established | CoA handling — fire 100k CoA from the RADIUS server side |
| 7 | `cold_start` | 1M | Full-scale acceptance run |
| 8 | `pessimal` | 100k | Repeat pessimal at higher count after full-scale passes |

**Why this order:** Problems at 1k are free to diagnose. Problems at 10k cost 10x more time. The 100k cold_start is the single most diagnostic run — it produces rich enough statistics to catch both average-case and tail-latency issues. Do not skip steps to jump to 1M.

**Run each scenario twice against each candidate** before comparing. The second run is your actual measurement; the first warms OS page caches and connection state on the RADIUS server side.

---

## 4. Per-scenario procedure

### 4.1 Before each run

```bash
# 1. Verify source IP aliases are still bound (they don't survive reboots)
ip addr show | grep -E '192\.168\.100\.'

# 2. Validate the scenario config
./bin/radstorm validate-config /data/scenarios/coldstart-100k.toml

# 3. Check free disk space (large runs produce large Parquet files)
df -h /data/results

# 4. Confirm the RADIUS server is accepting requests
echo "User-Name = testuser001, User-Password = testpass001" | \
  radclient 10.0.0.10:1812 auth mysecret

# 5. Note the start time and candidate version
date -u
ssh radius-server "freeradius -v" 2>&1 | head -1
```

### 4.2 `cold_start` procedure

Cold start simulates post-outage reconnect: subscribers activate following a Gaussian distribution centered at `mu_sec` with spread `sigma_sec`. This matches the real-world behaviour of CPE modems re-dialing after seeing the link come back up.

```bash
# Run the scenario
./bin/radstorm run-scenario \
  --config /data/scenarios/coldstart-100k.toml \
  --out /data/results/freeradius/coldstart-100k-run1 \
  --drain-seconds 60

# Capture outcome line (already printed to stdout on completion)
# Inspect immediately
./bin/radstorm analyze-results /data/results/freeradius/coldstart-100k-run1
```

Save the entire output directory as your artifact.

### 4.3 `uniform` procedure

Uniform spreads activations evenly over `duration_sec`. Use this for capacity sizing ("how many auth/sec can this server sustain?").

```bash
./bin/radstorm run-scenario \
  --config /data/scenarios/uniform-1k.toml \
  --out /data/results/freeradius/uniform-1k-run1

jq '.establishment.latency_ms' /data/results/freeradius/uniform-1k-run1/summary.json
```

### 4.4 `pessimal` procedure

Pessimal fires all subscribers simultaneously (`burst_window_ms = 0`). This is a worst-case stress, not a realistic scenario. Use it to surface server-side bottlenecks (thread contention, lock queuing, connection pool exhaustion) that cold_start's Gaussian spread might not hit.

**Do not run pessimal at 1M without first running cold_start at 1M successfully.** The burst will exhaust your port range and likely saturate the RADIUS server in ways that produce less useful signal than a ramped cold_start.

```bash
./bin/radstorm run-scenario \
  --config /data/scenarios/pessimal-10k.toml \
  --out /data/results/freeradius/pessimal-10k-run1

# Check still_in_flight_at_end — non-zero here means drain timeout was too short
jq '.subscribers.still_in_flight_at_end' \
  /data/results/freeradius/pessimal-10k-run1/summary.json
```

If `still_in_flight_at_end` is non-zero, increase `--drain-seconds` and re-run. This is not a server failure.

### 4.5 `coa_storm` procedure

The `coa_storm` scenario type configures radstorm's CoA listener and waits to receive server-initiated CoA-Request packets. The "storm" itself must be triggered externally — typically using `radclient` on the RADIUS server box to batch-fire CoA-Requests at the test host.

Step 1: Start a `coa_storm` scenario run on the test box:

```bash
./bin/radstorm run-scenario \
  --config /data/scenarios/coa-storm-100k.toml \
  --out /data/results/freeradius/coa-100k-run1 &

RUN_PID=$!
```

Step 2: On the RADIUS server box, fire CoA-Requests for each established subscriber:

```bash
# Example using FreeRADIUS radclient with a batch file
# /data/coa-batch.txt contains one CoA per line:
# Acct-Session-Id = "sess-000001", Huawei-Input-Peak-Rate = 20000000
radclient -f /data/coa-batch.txt 10.0.0.5:3799 coa mysecret
```

Step 3: Wait for the run to complete or manually terminate it:

```bash
wait $RUN_PID
./bin/radstorm analyze-results /data/results/freeradius/coa-100k-run1
```

Key metrics: `coa.received`, `coa.acked`, `coa.response_latency_us`.

### 4.6 Capturing artifacts

After each run, the output directory contains everything you need. Archive it before starting the next run:

```bash
CANDIDATE=freeradius
SCENARIO=coldstart-100k
RUN=run1

# Compress the entire directory (Parquet files compress well)
tar -czf /data/archive/${CANDIDATE}-${SCENARIO}-${RUN}.tar.gz \
  /data/results/${CANDIDATE}/${SCENARIO}-${RUN}/

# Keep summary.json separate for quick comparisons
cp /data/results/${CANDIDATE}/${SCENARIO}-${RUN}/summary.json \
  /data/summaries/${CANDIDATE}-${SCENARIO}-${RUN}.json
```

---

## 5. Comparing two candidates

### 5.1 Naming convention

Use a consistent directory structure across both candidates:

```
/data/results/
  freeradius/
    coldstart-1k-run1/
    coldstart-1k-run2/      ← use run2 as the measurement
    coldstart-10k-run2/
    coldstart-100k-run2/
    coldstart-1M-run2/
    pessimal-10k-run2/
    coa-100k-run2/
  interstellar/
    coldstart-1k-run1/
    coldstart-1k-run2/
    ...
```

Always use the second run of a scenario as your comparison data point (the first run is the warm-up). If the two runs for the same scenario differ by more than 20% on `time_to_full_ms`, investigate before proceeding — that variance is a signal, not noise.

### 5.2 Run the same scenario config against both candidates

The scenario TOML must be identical between candidates, except for the `[target]` block:

```toml
# freeradius-coldstart-100k.toml
[target]
auth_address  = "10.0.0.10:1812"
acct_address  = "10.0.0.10:1813"
shared_secret = "eval-secret-123"

# interstellar-coldstart-100k.toml
[target]
auth_address  = "10.0.0.20:1812"
acct_address  = "10.0.0.20:1813"
shared_secret = "eval-secret-123"
```

Every other parameter — subscriber count, activation schedule, NAS attributes, retransmit policy — must be byte-for-byte identical. Use `diff` to confirm before running:

```bash
diff freeradius-coldstart-100k.toml interstellar-coldstart-100k.toml
# Should show only [target] block differences
```

### 5.3 Produce a comparison

Use the comparison script included in this repo:

```bash
bash scripts/compare-runs.sh \
  /data/results/freeradius/coldstart-100k-run2 \
  /data/results/interstellar/coldstart-100k-run2
```

This prints a markdown table of key metric deltas. Copy-paste it into your evaluation report.

For the full suite of scenarios, run the comparison for each and collect all tables.

---

## 6. The decision matrix

Compare these fields across candidates. Numbers below are from the 100-subscriber validated run scaled conceptually — your actual numbers will differ.

| Metric | Where in summary.json | Weight | Notes |
|---|---|---|---|
| `establishment.time_to_full_ms` | `.establishment.time_to_full_ms` | High | Time for last subscriber to establish. Primary SLO metric. |
| `establishment.latency_ms.p99` | `.establishment.latency_ms.p99` | High | 99th-percentile auth+acct round-trip latency |
| `establishment.latency_ms.p999` | `.establishment.latency_ms.p999` | Medium | Tail latency — matters for the worst-hit subscribers |
| `subscribers.auth_failed` | `.subscribers.auth_failed` | Critical | Non-zero means the server rejected or dropped requests |
| `retransmits.total` | `.retransmits.total` | Medium | Non-zero means the server was slow enough to trigger retransmits |
| `server_health.unresponsive_periods` | `.server_health.unresponsive_periods` | Critical | Any non-empty array here is a serious problem |
| `coa.response_latency_us.p99` | `.coa.response_latency_us.p99` | Medium | CoA responsiveness during a CoA storm |
| `thresholds.overall` | `.thresholds.overall` | Pass/fail | Did the run meet all configured acceptance gates? |

**Weighting guidance:** A server that passes all thresholds but has a p999 100x worse than the other is likely better for production if `time_to_full_ms` and `auth_failed` are both acceptable. Tail latency matters for individual subscribers but not for the population-level SLO.

---

## 7. Acceptance criteria

These are the spec §4.2 thresholds radstorm enforces automatically via the `thresholds` block. A run that fails any of these is a failing run regardless of other numbers.

| Threshold | Value | Meaning |
|---|---|---|
| All subscribers established | `established_count == total` | Zero tolerance for auth failures in a healthy-server evaluation run |
| p99 establishment latency | ≤ 5000 ms | 99% of subscribers establish within 5 seconds of their activation |
| `still_in_flight_at_end` | 0 | No subscriber left hanging at run end |
| `server_health.unresponsive_periods` | empty | Server never went dark during the run |

**Operator interpretation:**

- `established_count == total`: If this fails, check `auth_failed` vs `acct_failed`. Auth failures mean the server is rejecting credentials or is unreachable. Acct failures (auth passed but accounting timed out) mean the accounting port is a bottleneck.
- p99 ≤ 5000 ms: The 5-second threshold is generous at small scale (100 subs at 10ms p99 easily passes). At 1M with a Gaussian ramp, p99 may legitimately rise to 2–3 seconds if the ramp is steeper than the server's processing rate. A p99 at 4800ms should trigger investigation even if it technically passes.
- `unresponsive_periods`: The server health monitor watches for reply gaps exceeding 5 seconds. Any period here means the server stopped responding entirely for ≥5 seconds. In a reconnect storm, this is what causes cascade failure — subscribers that were waiting for a reply have to retransmit, compounding the load.

---

## 8. Reporting

### 8.1 Artifacts to share

For each scenario and each candidate, collect:

1. `summary.json` — the machine-readable result
2. `summary.txt` — human-readable formatted result
3. `config.toml` — frozen copy of the config used (proves reproducibility)
4. `run.log` — for the evaluator to inspect if a threshold failed

### 8.2 Comparison table format

Run `scripts/compare-runs.sh` for each scenario pair. The output is a markdown table ready to paste into a report or wiki.

### 8.3 Summary for stakeholders

Structure your report as:

1. **Test configuration** — hardware specs, software versions, scenario parameters, date/time of each run
2. **Scenario results table** — one row per scenario, columns for each candidate's key metrics
3. **Comparison tables** — output of `compare-runs.sh` for each scenario
4. **Threshold pass/fail matrix** — did each candidate pass each threshold at each scale?
5. **Recommendation** — state which candidate you recommend and the specific metrics that drove the conclusion
6. **Reproduction instructions** — the exact commands to re-run any scenario, including the exact config file used

For runs at 1M scale: explicitly note in the report that this is the architectural target and whether it has been validated on reference hardware. Do not present 1M results extrapolated from smaller runs as measured data.

---

**Next:** [docs/COMPARISON-WORKFLOW.md](COMPARISON-WORKFLOW.md) — automation for running the full comparison campaign.
