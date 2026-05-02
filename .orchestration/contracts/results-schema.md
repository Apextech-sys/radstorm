# Contract: Results schema (summary.json)

**Owners:** `pkg/collector` (writes), `apps/api` (serves), `apps/web` (renders).

---

## summary.json

Written at the end of every run to `<output-dir>/summary.json`.

```json
{
  "run_id": "run-2026-05-02T01-23-45Z-7f3a",
  "config_hash": "sha256:...",
  "started_at": "2026-05-02T01:23:45.123Z",
  "finished_at": "2026-05-02T01:24:30.456Z",
  "duration_ms": 45333,
  "scenario_type": "cold_start",
  "outcome": "succeeded",                       // succeeded | failed | cancelled

  "subscribers": {
    "total": 1000,
    "established": 998,
    "auth_failed": 1,
    "acct_failed": 1,
    "terminated": 0,
    "still_in_flight_at_end": 0
  },

  "establishment": {
    "time_to_first_ms": 87,
    "time_to_full_ms": 32450,
    "latency_ms": {
      "min": 12,
      "p50": 145,
      "p95": 890,
      "p99": 2300,
      "p999": 4100,
      "max": 4500
    },
    "curve": [
      { "offset_ms": 0,     "activated": 0,    "established": 0,    "in_flight": 0 },
      { "offset_ms": 1000,  "activated": 18,   "established": 12,   "in_flight": 6 }
    ]
  },

  "retransmits": {
    "total": 27,
    "subscribers_with_retransmit": 24,
    "per_subscriber_distribution": {
      "p50": 0,
      "p95": 0,
      "p99": 1,
      "max": 3
    }
  },

  "coa": {
    "received": 0,
    "acked": 0,
    "naked": 0,
    "dropped": 0,
    "response_latency_us": null
  },

  "disconnect": {
    "received": 0,
    "acked": 0,
    "naked": 0,
    "response_latency_us": null
  },

  "server_health": {
    "unresponsive_periods": [],
    "error_responses": 0
  },

  "thresholds": {
    "evaluated": [
      { "name": "all_established", "expected": "established_count == total", "actual": "998 == 1000", "pass": false },
      { "name": "p99_latency_ms", "expected": "<= 5000", "actual": 2300, "pass": true }
    ],
    "overall": "fail"
  },

  "artifacts": {
    "events_parquet": "events.parquet",
    "subscribers_parquet": "subscribers.parquet",
    "run_log": "run.log"
  },

  "measurement_integrity": {
    "trust": "high",
    "events_submitted": 8742,
    "events_dropped_back_pressure": 0,
    "outcomes_dropped_back_pressure": 0,
    "events_dropped_after_stop": 0,
    "first_drop_offset_ms": null,
    "last_drop_offset_ms": null,
    "notes": [
      "Latency captured with monotonic clock immediately on packet receipt.",
      "Userspace timestamps via Go net.UDPConn — kernel scheduling jitter floor ≈50–200µs on a clean Linux box."
    ]
  }
}
```

## measurement_integrity

This section reports signals an operator can use to decide how much to trust the latency aggregations in the same summary file.

| Field | Type | Meaning |
|---|---|---|
| `trust` | `"high" \| "medium" \| "low"` | Heuristic rollup. high = 0 drops; medium = <1% drops; low = ≥1% drops. |
| `events_submitted` | int | Events that successfully landed on a shard channel. Denominator for drop rate. |
| `events_dropped_back_pressure` | int | Events that Submit() refused because the shard buffer was full. Latency aggregations are computed over a partial event stream when this is non-zero. |
| `outcomes_dropped_back_pressure` | int | Same for SubmitOutcome (per-subscriber outcome rows). |
| `events_dropped_after_stop` | int | Events submitted after Stop() — usually a code-path bug; should be 0 in healthy runs. |
| `first_drop_offset_ms` | int \| null | T0-relative time of the first back-pressure drop. Lets operators correlate with what the test was doing at that moment. |
| `last_drop_offset_ms` | int \| null | Same for most recent drop. |
| `notes` | string[] | Freeform human-readable caveats. Always includes the measurement-floor disclaimer; appends a guidance note when drops occurred. |

### Trust semantics

- **`high`** — 0 events dropped. Numbers are reliable to the userspace-timestamp floor (~50–200µs).
- **`medium`** — <1% of submitted events dropped. Numbers are usable but tail percentiles may be slightly understated (dropped events tend to be the rare slow ones whose Submit happened during a flush burst).
- **`low`** — ≥1% dropped. Treat tail percentiles with significant care. Re-run with a faster output disk (NVMe required for Tier 3), more CPU cores (more shards = more parallel drain), or a smaller subscriber count.

### Why drop-on-full

The collector deliberately drops events on full channels rather than blocking the submitter. Blocking would back-pressure the receiver/sender goroutines, which would skew the latency measurements of subsequent packets. Losing event-log completeness is acceptable; corrupting measurements is not. The integrity counters surface the trade-off explicitly.

## Latency distributions

All latency fields use **microseconds** for `*_us` and **milliseconds** for `*_ms`. Fixed: never mix units within a field name.

Distributions reported as min/p50/p95/p99/p99.9/max.

## Threshold evaluation

The collector evaluates thresholds defined in the scenario config (or defaults from spec §4.2) and reports per-threshold pass/fail plus an `overall` rollup.

## Files written to run directory

| File | Contents |
|---|---|
| `summary.json` | This document |
| `summary.txt` | Human-readable formatted version |
| `events.parquet` | Per-event log (one row per packet sent/received per state change) |
| `subscribers.parquet` | Per-subscriber outcome (one row per subscriber) |
| `progress.jsonl` | Per-second progress snapshots (used by API server for SSE) |
| `run.log` | Operational log of the harness itself (slog JSON) |
| `config.toml` | Frozen copy of the input config |
