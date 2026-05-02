<!-- Purpose: Walkthrough for reading and interpreting a radstorm summary.json, with annotated examples and DuckDB query recipes. -->

# Results Interpretation

Audience: operator who has completed a run and wants to understand what the numbers mean before writing a comparison report.

---

## Table of contents

1. [Annotated summary.json examples](#1-annotated-summaryjson-examples)
2. [Establishment curve interpretation](#2-establishment-curve-interpretation)
3. [Latency distribution interpretation](#3-latency-distribution-interpretation)
4. [Retransmit interpretation](#4-retransmit-interpretation)
5. [CoA latency interpretation](#5-coa-latency-interpretation)
6. [server_health.unresponsive_periods](#6-server_healthunresponsive_periods)
7. [Threshold rollup interpretation](#7-threshold-rollup-interpretation)
8. [DuckDB: querying subscribers.parquet](#8-duckdb-querying-subscribersparquet)
9. [DuckDB: querying events.parquet shards](#9-duckdb-querying-eventsparquet-shards)

---

## 1. Annotated summary.json examples

### 1.1 A healthy run (100k cold_start, good server)

```json
{
  "run_id": "run-2026-05-02T08-00-00Z-a1b2",
  "duration_ms": 382450,                         // 6 min 22 sec — within the ramp window

  "outcome": "succeeded",                         // ✓ No harness-level error

  "subscribers": {
    "total": 100000,
    "established": 100000,                        // ✓ Perfect establishment rate
    "auth_failed": 0,                             // ✓ Zero auth failures
    "acct_failed": 0,                             // ✓ Zero accounting failures
    "still_in_flight_at_end": 0                   // ✓ Fully drained
  },

  "establishment": {
    "time_to_first_ms": 2341,                     // First subscriber established at 2.3s
    "time_to_full_ms": 378200,                    // Last subscriber established at 6m18s
    "latency_ms": {
      "min": 8,
      "p50": 145,                                 // Median: 145ms — reasonable for 100k load
      "p95": 890,                                 // 95th pct: under 1 second
      "p99": 2300,                                // 99th pct: 2.3 seconds — passes 5s threshold
      "p999": 4100,                               // 99.9th pct: 4.1 seconds — tail is long but not broken
      "max": 4500
    }
  },

  "retransmits": {
    "total": 27,                                  // 27 retransmits across the entire run
    "subscribers_with_retransmit": 24             // 0.024% of subscribers — acceptable
  },

  "server_health": {
    "unresponsive_periods": [],                   // ✓ Server never went dark
    "error_responses": 0                          // ✓ No Access-Reject
  },

  "thresholds": {
    "overall": "pass"                             // ✓ All gates cleared
  }
}
```

**Interpretation:** This is a healthy run. The server handled 100k subscribers with zero failures, acceptable p99 latency, and negligible retransmits. The 27 retransmits suggest occasional processing delays under peak load — worth monitoring at higher scale but not actionable here.

---

### 1.2 A concerning run (same scenario, struggling server)

```json
{
  "run_id": "run-2026-05-02T09-00-00Z-c3d4",
  "duration_ms": 598100,                         // 10 min — approaching hard_timeout

  "outcome": "succeeded",                         // "succeeded" here means no harness error
                                                  // — look at the thresholds block for the real verdict

  "subscribers": {
    "total": 100000,
    "established": 98421,                         // ⚠ 1579 subscribers never established
    "auth_failed": 1102,                          // ⚠ Server rejected or dropped 1102 auth requests
    "acct_failed": 477,                           // ⚠ Auth passed but accounting timed out for 477
    "still_in_flight_at_end": 156                 // ⚠ 156 requests still in-flight at run end (drain too short)
  },

  "establishment": {
    "time_to_first_ms": 3210,
    "time_to_full_ms": 591000,                    // ⚠ Nearly 10 minutes — close to hard_timeout
    "latency_ms": {
      "min": 12,
      "p50": 890,                                 // Median 890ms — server under heavy load
      "p95": 4200,                                // ⚠ 4.2s at p95 — only 5% of subscribers below this
      "p99": 6800,                                // ⚠ Fails the 5000ms threshold
      "p999": 14200,                              // ⚠ 14.2 seconds for the worst subscribers
      "max": 22000
    }
  },

  "retransmits": {
    "total": 4823,                                // ⚠ 4823 retransmits — server was routinely slow
    "subscribers_with_retransmit": 3901           // 3.9% of subscribers retransmitted at least once
  },

  "server_health": {
    "unresponsive_periods": [
      { "start_ms": 45000, "end_ms": 52000 },    // ⚠ Server went dark for 7 seconds at t=45s
      { "start_ms": 198000, "end_ms": 201000 }   // ⚠ 3-second gap at t=198s
    ],
    "error_responses": 89                         // ⚠ Server sent 89 Access-Reject responses
  },

  "thresholds": {
    "evaluated": [
      { "name": "all_established", "expected": "100000 == 100000", "actual": "98421 == 100000", "pass": false },
      { "name": "p99_latency_ms", "expected": "<= 5000", "actual": 6800, "pass": false }
    ],
    "overall": "fail"                             // ✗ Failed two thresholds
  }
}
```

**Interpretation:** The server struggled severely. Two unresponsive periods (at t=45s, near the peak of the Gaussian ramp) suggest the server hit a bottleneck exactly when load was highest — classic cascade failure. The retransmit storm from those 7 seconds of darkness compounds the load, producing the long tail at p99/p999. The `auth_failed` vs `acct_failed` split tells you: 1102 subscribers couldn't even authenticate (server too busy or policy rejection), while 477 authenticated but then the accounting port timed out (accounting path is a separate bottleneck).

---

## 2. Establishment curve interpretation

The `establishment.curve` array is a time series sampled every second:

```json
"curve": [
  { "offset_ms": 0,      "activated": 0,     "established": 0,    "in_flight": 0 },
  { "offset_ms": 1000,   "activated": 18,    "established": 12,   "in_flight": 6 },
  { "offset_ms": 2000,   "activated": 45,    "established": 38,   "in_flight": 13 },
  ...
  { "offset_ms": 120000, "activated": 49820, "established": 49650, "in_flight": 170 },   // peak of ramp
  { "offset_ms": 121000, "activated": 50100, "established": 50001, "in_flight": 99 },    // ramp declining
  ...
]
```

**Healthy pattern:** `established` closely tracks `activated`. The `in_flight` count rises with the ramp, peaks at the same time as activation, and falls afterward as replies arrive. `in_flight` should return to ~0 well before the run ends.

**Cascade failure pattern:** `established` falls behind `activated` during the ramp. `in_flight` keeps rising after the activation peak (because retransmits are adding new IDs even as the ramp winds down). `in_flight` may stay elevated for the rest of the run.

Use the curve to locate the failure point:

```bash
# Find the offset_ms where in_flight exceeded 10,000 for the first time
jq '.establishment.curve[] | select(.in_flight > 10000) | .offset_ms' summary.json | head -1
```

---

## 3. Latency distribution interpretation

All latency fields are end-to-end establishment latency: time from when the subscriber sends its first Access-Request to when it receives the Accounting-Response (or Access-Reject / Accounting timeout).

| Percentile | What it tells you |
|---|---|
| p50 (median) | What a typical subscriber experiences. The floor of the distribution. |
| p95 | 95% of subscribers completed within this time. |
| p99 | Your SLO threshold. 1 in 100 subscribers waited this long. |
| p999 | 1 in 1000 subscribers — the worst-hit 0.1%. At 1M subs, this is 1000 real CPEs. |
| max | Single worst subscriber. Often an outlier; don't use this as the primary metric. |

**When long-tail matters:** At 1M subscribers, a p999 of 30 seconds means 1000 subscribers waited 30+ seconds. If those are real customers, that is 1000 failed logins. For evaluation decisions, use p99 as the primary metric but note if p999 / p99 > 5 — a large ratio indicates a bimodal distribution (most are fast, a small group is very slow), which is often a specific bottleneck rather than general load.

**Comparing candidates:** A candidate with p99=2000ms and p999=4000ms is generally better than one with p99=1800ms and p999=15000ms — the second candidate has a worse tail even though its median is slightly better.

---

## 4. Retransmit interpretation

```json
"retransmits": {
  "total": 27,
  "subscribers_with_retransmit": 24,
  "per_subscriber_distribution": {
    "p50": 0,
    "p95": 0,
    "p99": 1,
    "max": 3
  }
}
```

**What causes retransmits:** The RADIUS server did not reply within `retransmit.initial_timeout_ms` (default 5 seconds). radstorm allocates a new Identifier and resends. The server may have been busy, dropped the packet, or sent a reply that arrived after the timeout.

**What is acceptable:** In a healthy run at small-to-medium scale, zero retransmits is normal. At 100k+ with a production-grade server, occasional retransmits (< 0.1% of subscribers) are acceptable. The key question is whether retransmit rate scales with subscriber count (a load issue) or stays flat (an occasional timing issue).

**Red flags:**
- `subscribers_with_retransmit > 1%` — the server is consistently slow
- `per_subscriber_distribution.max > 3` — some subscribers hit all three retransmit slots, meaning they waited 5s + 6s + 10s + another 5s = 26 seconds before being marked `auth_failed`
- Retransmit count spikes after `server_health.unresponsive_periods` — the unresponsive period triggered a mass retransmit storm that compounded load

---

## 5. CoA latency interpretation

```json
"coa": {
  "received": 100000,
  "acked": 99987,
  "naked": 13,
  "dropped": 0,
  "response_latency_us": {
    "min": 120,
    "p50": 450,
    "p95": 2100,
    "p99": 8900,
    "p999": 15000,
    "max": 22000
  }
}
```

CoA latency is measured in **microseconds** (`_us`). A p99 of 8900µs = 8.9ms.

**acked vs naked:** `acked` means radstorm recognized the subscriber and sent CoA-ACK. `naked` means the subscriber lookup failed (session not found) and radstorm sent CoA-NAK with Error-Cause 503. A few NAKs are expected if CoA-Requests arrive for subscribers that have already terminated. A high NAK rate indicates a session lookup problem.

**dropped:** Packets that arrived but could not be parsed or validated. Should be zero unless there is a shared-secret mismatch or a buggy CoA sender.

**Response latency:** This measures how quickly radstorm processed and replied to each CoA-Request. Since radstorm is the responder here, this latency is dominated by the lookup in the subscriber pool and the packet encoding time — it should be very fast (sub-millisecond p50). High CoA response latency (p99 > 10ms) suggests the test box is CPU-saturated.

---

## 6. `server_health.unresponsive_periods`

```json
"server_health": {
  "unresponsive_periods": [
    { "start_ms": 45000, "end_ms": 52000, "duration_ms": 7000 }
  ]
}
```

An unresponsive period is detected when the server sends no replies for ≥5 seconds while there are in-flight requests. This threshold is hardcoded in the collector.

**What they mean:** The server completely stopped responding. Common causes:
- Thread pool exhaustion (all threads busy processing or waiting on DB)
- Memory pressure / GC pause (Java-based servers)
- Lock contention on the user database
- The server crashed and restarted

**Impact:** Every in-flight subscriber during the unresponsive period will eventually retransmit (after `initial_timeout_ms`). If the unresponsive period is longer than the retransmit timeout, subscribers start being marked `auth_failed`. The retransmit storm after the server recovers can trigger a second unresponsive period — this is cascade failure.

**Correlating with the curve:** Find the `start_ms` of the unresponsive period in the establishment curve. You should see `in_flight` rising sharply at that offset as no new replies are arriving, followed by a spike in retransmits when the retransmit timeout fires.

---

## 7. Threshold rollup interpretation

```json
"thresholds": {
  "evaluated": [
    { "name": "all_established", "expected": "established_count == total",
      "actual": "100000 == 100000", "pass": true },
    { "name": "p99_latency_ms", "expected": "<= 5000",
      "actual": 2300, "pass": true }
  ],
  "overall": "pass"
}
```

`overall` is `pass` only when every evaluated threshold passes. The CLI exits with code `2` when `overall = fail`.

The `actual` field shows the measured value. Use it to understand how much margin you have against the threshold — `actual: 4900` against `expected: <= 5000` is passing but marginal.

---

## 8. DuckDB: querying subscribers.parquet

`subscribers.parquet` contains one row per virtual subscriber with their final outcome and latency. It is the most useful file for per-subscriber analysis.

Schema (key columns): `subscriber_id`, `username`, `final_state`, `establishment_latency_ms`, `retransmit_count`, `auth_attempts`, `activated_at_ms`, `established_at_ms`.

### Ready-to-paste queries

**Query 1: Count by final state**
```sql
SELECT final_state, count(*) AS n, round(count(*) * 100.0 / sum(count(*)) over(), 2) AS pct
FROM 'results/run-id/subscribers.parquet'
GROUP BY final_state
ORDER BY n DESC;
```

**Query 2: Latency percentiles for established subscribers**
```sql
SELECT
  min(establishment_latency_ms)                                   AS min_ms,
  quantile_cont(establishment_latency_ms, 0.50)                  AS p50_ms,
  quantile_cont(establishment_latency_ms, 0.95)                  AS p95_ms,
  quantile_cont(establishment_latency_ms, 0.99)                  AS p99_ms,
  quantile_cont(establishment_latency_ms, 0.999)                 AS p999_ms,
  max(establishment_latency_ms)                                   AS max_ms
FROM 'results/run-id/subscribers.parquet'
WHERE final_state = 'established';
```

**Query 3: Subscribers that retransmitted at least once**
```sql
SELECT username, retransmit_count, establishment_latency_ms, final_state
FROM 'results/run-id/subscribers.parquet'
WHERE retransmit_count > 0
ORDER BY retransmit_count DESC, establishment_latency_ms DESC
LIMIT 50;
```

**Query 4: Latency histogram (100ms buckets)**
```sql
SELECT
  floor(establishment_latency_ms / 100) * 100 AS bucket_ms,
  count(*) AS n
FROM 'results/run-id/subscribers.parquet'
WHERE final_state = 'established'
GROUP BY 1
ORDER BY 1;
```

**Query 5: Failed subscribers with auth method**
```sql
SELECT final_state, auth_attempts, count(*) AS n
FROM 'results/run-id/subscribers.parquet'
WHERE final_state NOT IN ('established', 'terminated')
GROUP BY 1, 2
ORDER BY n DESC;
```

---

## 9. DuckDB: querying events.parquet shards

`events-shard-*.parquet` contain one row per packet event. There may be many shards (one per CPU core). DuckDB handles glob patterns, so you do not need to merge them.

Schema (key columns): `event_type`, `subscriber_id`, `timestamp_us`, `packet_code`, `identifier`, `src_ip`, `src_port`, `dst_ip`, `dst_port`, `latency_us`, `is_retransmit`, `error`.

### Ready-to-paste queries

**Query 1: Event type breakdown**
```sql
SELECT event_type, count(*) AS n
FROM 'results/run-id/events-shard-*.parquet'
GROUP BY event_type
ORDER BY n DESC;
```

**Query 2: Timeline of events per second (for a specific subscriber)**
```sql
SELECT
  floor(timestamp_us / 1000000) AS second_offset,
  event_type,
  packet_code,
  identifier,
  latency_us
FROM 'results/run-id/events-shard-*.parquet'
WHERE subscriber_id = 'sub00000042'
ORDER BY timestamp_us;
```

**Query 3: Peak concurrency (in-flight count per second)**
```sql
-- Approximation: count auth_sent events minus auth_accepted events per second
SELECT
  floor(timestamp_us / 1000000) AS second,
  sum(CASE WHEN event_type = 'auth_sent' THEN 1 ELSE 0 END) AS sent,
  sum(CASE WHEN event_type = 'auth_accepted' THEN 1 ELSE 0 END) AS accepted,
  sum(CASE WHEN event_type = 'auth_sent' THEN 1 ELSE 0 END) -
    sum(CASE WHEN event_type = 'auth_accepted' THEN 1 ELSE 0 END) AS delta
FROM 'results/run-id/events-shard-*.parquet'
GROUP BY 1
ORDER BY 1;
```

Replace `results/run-id/` with the actual path to your run's output directory.

---

**Next:** [docs/COMPARISON-WORKFLOW.md](COMPARISON-WORKFLOW.md) — automating comparison between two candidates.
