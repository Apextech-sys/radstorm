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
  }
}
```

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
