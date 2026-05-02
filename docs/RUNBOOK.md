# radstorm Operator Runbook

<!-- Purpose: Step-by-step operator reference for building, configuring, running, and operating radstorm in production and local environments. -->

## Table of contents

1. [Prerequisites](#prerequisites)
2. [Building](#building)
3. [First run](#first-run)
4. [Interpreting summary.json](#interpreting-summaryjson)
5. [Configuration reference](#configuration-reference)
6. [Source IP setup for large-scale tests](#source-ip-setup)
7. [Running the API and frontend](#running-the-api-and-frontend)
8. [Production deployment](#production-deployment)
9. [Cancellation](#cancellation)
10. [Output artifacts](#output-artifacts)

---

## Prerequisites

### Production target

Linux x86_64. Everything in this runbook assumes Linux unless noted. The CLI and API server compile and run on Windows and macOS but those are development platforms only.

### Required tools

| Tool | Minimum version | Purpose |
|---|---|---|
| Go | 1.22+ (tested on 1.26.2) | Build CLI + API |
| Docker + Compose | Docker Engine 24+ / Compose v2 | Local FreeRADIUS test rig |
| Node.js + npm | Node 20+ (tested on 24) | Frontend build |
| jq | Any modern version | E2E assertions, result inspection |
| curl | Any | E2E API flows, manual testing |

All must be in `PATH`. The E2E orchestrator checks for them and exits early with a clear error if any are absent.

### Network requirements

The test machine must be able to send UDP packets to the RADIUS server's auth and accounting ports (default 1812/1813). For the Docker rig, the ports are mapped to 11812/11813 on the loopback interface. Firewall rules must allow outbound UDP on the configured port range (`source.port_range`).

---

## Building

```bash
# From repo root

# Build the CLI (bin/radstorm) and API server (bin/radstorm-api)
make build

# Build only the CLI
make build-cli

# Build only the API
make build-api

# Bring up the local FreeRADIUS Docker rig (auth: udp/11812, acct: udp/11813)
make docker-up

# Run all Go unit tests
make test-go

# Run the full E2E suite (requires Docker)
make e2e
```

The binaries land in `bin/`. They are statically linked (pure Go, no CGO). Copy the binary to the test host — no runtime dependencies.

---

## First run

This walks through a 100-subscriber smoke run against the local Docker FreeRADIUS rig. The rig must be healthy before proceeding.

```bash
# From repo root

# 1. Start the Docker rig and wait for it to become healthy (~15-30 seconds)
make docker-up
docker ps | grep freeradius   # should show "(healthy)"

# 2. Build the CLI
make build-cli

# 3. Run the smoke scenario
./bin/radstorm run-scenario \
  --config test/fixtures/scenarios/smoke-100.toml \
  --out results/first

# 4. Check the outcome line (printed to stdout on completion)
#    Example: run run-1777705832 — outcome=succeeded subscribers=100/100 duration=5668ms

# 5. Inspect the summary
jq . results/first/summary.json
```

The smoke scenario uses 100 subscribers from `test/fixtures/credentials/smoke-100.csv` against `127.0.0.1:11812` (auth) and `127.0.0.1:11813` (acct) with shared secret `testing123`.

Expected outcome: `outcome=succeeded`, `subscribers.established==100`, duration well under 30 seconds.

---

## Interpreting summary.json

`summary.json` is the canonical output of every run. Example from the 100-subscriber smoke run:

```json
{
  "run_id": "run-2026-05-02T01-23-45Z-7f3a",
  "started_at": "2026-05-02T01:23:45.123Z",
  "finished_at": "2026-05-02T01:24:30.456Z",
  "duration_ms": 5668,
  "scenario_type": "cold_start",
  "outcome": "succeeded",

  "subscribers": {
    "total": 100,
    "established": 100,
    "auth_failed": 0,
    "acct_failed": 0,
    "terminated": 0,
    "still_in_flight_at_end": 0
  },

  "establishment": {
    "time_to_first_ms": 87,
    "time_to_full_ms": 5200,
    "latency_ms": {
      "min": 3,
      "p50": 10,
      "p95": 28,
      "p99": 35,
      "p999": 41,
      "max": 45
    },
    "curve": [...]
  },

  "retransmits": {
    "total": 0,
    "subscribers_with_retransmit": 0
  },

  "thresholds": {
    "evaluated": [
      { "name": "all_established", "expected": "established_count == total",
        "actual": "100 == 100", "pass": true },
      { "name": "p99_latency_ms", "expected": "<= 5000",
        "actual": 35, "pass": true }
    ],
    "overall": "pass"
  }
}
```

### Key fields

| Field | Meaning | What to check |
|---|---|---|
| `outcome` | `succeeded` / `failed` / `cancelled` | First thing to look at |
| `subscribers.established` | Count that reached the `established` FSM state (auth-accept + acct-start response received) | Should equal `subscribers.total` for healthy server |
| `subscribers.auth_failed` | Timed out or received Access-Reject during auth | Non-zero indicates server-side auth issues or secret mismatch |
| `subscribers.acct_failed` | Timed out during accounting | Non-zero often means accounting port unreachable |
| `establishment.latency_ms.p99` | 99th-percentile end-to-end (auth + acct) latency in ms | Compare against your SLA |
| `retransmits.total` | Total retransmit events | Non-zero with no failures means server is slow, not broken |
| `thresholds.overall` | Aggregate pass/fail from configured thresholds | `pass` = all gates met |
| `duration_ms` | Wall-clock duration of the test | At 100 subs/smoke: ~5-10 seconds |

### Threshold evaluation

The `thresholds` block shows every configured gate evaluated against actual results. `overall` is `pass` only when every gate passes. The CLI exits with code `2` when thresholds fail (exit `0` = pass, exit `1` = harness error, exit `2` = threshold fail).

---

## Configuration reference

Configurations are TOML files. Run `./bin/radstorm validate-config <path>` before any test to catch errors early.

### `[target]`

```toml
[target]
auth_address  = "127.0.0.1:11812"   # RADIUS auth server (UDP)
acct_address  = "127.0.0.1:11813"   # RADIUS acct server (UDP)
shared_secret = "testing123"        # Shared secret — must match server
```

**Operator advice:** Use the production RADIUS server address and the real shared secret. Never use the Docker rig address/secret against a production server. The shared secret is never logged.

### `[coa_listener]`

```toml
[coa_listener]
bind_address  = "0.0.0.0:3799"     # radstorm listens for server-initiated CoA/Disconnect
shared_secret = "testing123"
```

**Operator advice:** Port 3799 is the RFC 5176 default for Dynamic Authorization. The host must have this port open to inbound UDP from the RADIUS server. If you are not testing CoA/Disconnect flows, the listener still starts (it is always present) but receives nothing.

### `[subscribers]`

```toml
[subscribers]
count               = 100           # Virtual subscribers to activate
credentials_file    = "test/fixtures/credentials/smoke-100.csv"
auth_method_pap_pct = 100           # 100 = all PAP; 0 = all CHAP; 50 = mixed
type_pppoe_pct      = 100           # 100 = all PPPoE; 0 = all MAC-auth
include_acct_start  = true          # Set false for auth-only (no Accounting-Request)
```

**Operator advice:** `credentials_file` is resolved against the current working directory. Use an absolute path when in doubt. The file must have at least `count` rows. See [Troubleshooting](#troubleshooting) for the "file not found" error.

For scale tests, generate credentials with the provided tool (`make gen-credentials COUNT=1000000`). Do not commit multi-million-row credential files to the repo.

### `[source]`

```toml
[source]
ips        = ["127.0.0.1"]         # Source IPs already bound on this host
port_range = [10000, 60000]         # Ephemeral port range to use
```

**Operator advice:** For small tests (≤5000 subs), a single loopback or host IP with the default port range is sufficient. For scale tests, see [Source IP setup](#source-ip-setup).

### `[nas]`

```toml
[nas]
ip_address = "10.0.0.1"            # NAS-IP-Address attribute in every Access-Request
identifier = "radstorm-test-nas"   # NAS-Identifier
# huawei = { connect_id = "1/1/1.100" }  # Optional Huawei VSAs
```

**Operator advice:** Set `nas.ip_address` to an IP the RADIUS server will recognize as a valid NAS. For the Docker test rig, any value works. For a real server, this must match a NAS entry.

### `[retransmit]`

```toml
[retransmit]
initial_timeout_ms = 5000          # Wait before first retransmit
max_retries        = 3             # Total retransmits per request
backoff            = "exponential" # exponential | linear | constant
backoff_base_ms    = 1000          # Base for exponential (1s, 2s, 4s)
```

**Operator advice:** Default values are conservative and RFC-aligned. For stress testing a slow server, reduce `initial_timeout_ms` to 2000 to surface timeout issues faster. For production validation, keep defaults to match real NAS behaviour.

### `[scenario]`

```toml
[scenario]
type             = "cold_start"    # cold_start | uniform | pessimal | coa_storm
hard_timeout_sec = 300             # Abort the run if not finished within this

[scenario.cold_start]
mu_sec         = 20.0              # Mean activation time (Gaussian centre)
sigma_sec      = 8.0               # Std dev — controls ramp width
truncate_sigma = 3.0               # Clamp at ±(truncate_sigma * sigma)

[scenario.uniform]
duration_sec = 10.0                # Spread activations evenly over this window

[scenario.pessimal]
burst_window_ms = 0                # 0 = simultaneous burst (worst case)
```

**Operator advice on sizing:**

| Scenario type | Typical use | `mu_sec` guidance |
|---|---|---|
| `cold_start` | Simulates post-outage reconnect storm with Gaussian ramp | `mu_sec = total_subs / expected_auth_rate_per_sec` |
| `uniform` | Steady ramp — good for capacity sizing | `duration_sec` = total ramp window |
| `pessimal` | All subscribers activate simultaneously | Use only with small counts; this will saturate any server |

### `[output]`

```toml
[output]
directory         = "results/"     # Output directory (created if absent)
flush_interval_sec = 30            # How often to flush Parquet buffers to disk
```

**Operator advice:** For runs that produce millions of events, keep `flush_interval_sec` at 30 or lower to limit memory use. The directory must be writable. Use an absolute path for production.

---

## Source IP setup

The RADIUS Identifier field is 8 bits, limiting concurrent in-flight requests to 256 per `(srcIP, srcPort, dstIP, dstPort)` 4-tuple. To support large numbers of concurrent in-flight sessions, you need multiple 4-tuples.

### Capacity math

```
max_concurrent_inflight = num_src_ips × ports_per_ip × 256

Example: 32 source IPs × ~125 ports each × 256 IDs = ~1,024,000 concurrent
```

With the default port range `[10000, 60000]` = 50,000 ports distributed across all source IPs. A single source IP gives 50,000 ports × 256 = ~12.8M theoretical maximum, but socket overhead becomes the constraint at scale.

### Adding source IP aliases (Linux)

```bash
# Add aliases on the loopback (or use a real interface for production)
for i in $(seq 1 31); do
    sudo ip addr add 127.0.0.${i}/8 dev lo
done

# Verify
ip addr show lo | grep "127.0.0\."
```

Then set in your config:

```toml
[source]
ips = [
  "127.0.0.1",  "127.0.0.2",  "127.0.0.3",  "127.0.0.4",
  "127.0.0.5",  "127.0.0.6",  "127.0.0.7",  "127.0.0.8",
  # ... up to 127.0.0.32
]
port_range = [10000, 60000]
```

### sysctl tuning for large tests

```bash
# Widen the ephemeral port range used by the OS (applies to all sockets)
sudo sysctl -w net.ipv4.ip_local_port_range="10000 60000"

# Increase socket send/receive buffers
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.wmem_max=134217728
sudo sysctl -w net.core.rmem_default=33554432

# Make these persistent
echo "net.ipv4.ip_local_port_range = 10000 60000" | sudo tee -a /etc/sysctl.conf
echo "net.core.rmem_max = 134217728" | sudo tee -a /etc/sysctl.conf
sudo sysctl -p
```

### ID exhaustion

If you see `ErrIdentifierExhausted` in the run log, all 256 IDs in every available 4-tuple are in use simultaneously. Remedies in order of preference:

1. Add more source IP aliases (see above)
2. Widen `port_range` to give each IP more source ports
3. Reduce `subscribers.count` to lower the concurrency demand
4. Increase `retransmit.initial_timeout_ms` to reduce the time each ID stays in-flight

For a 1M-subscriber run at realistic ISP concurrency (~500 in-flight per IP), 32 source IPs is the minimum. For burst/pessimal scenarios at full concurrency, 64+ IPs may be needed.

---

## Running the API and frontend

```bash
# Start API (port 8080) + Next.js dev server (port 3000) in one shot
make dev

# Or start each independently:
# API server
export RADSTORM_API_ADDR=:8080
export RADSTORM_DATA_DIR=./data
export RADSTORM_CLI_BIN=./bin/radstorm
./bin/radstorm-api

# Frontend (separate terminal)
cd apps/web && npm run dev
```

Then open `http://localhost:3000`.

### Navigating the frontend

1. **`/runs/new`** — fill in the scenario configuration form. Use a template (smoke-100, cold-start-1k) as a starting point. Hit "Start Run".
2. **`/runs/{id}`** — live progress view. Shows establishment curve, in-flight count, stat cards, and a collapsible log panel. The SSE stream updates every second.
3. Once the run completes, the page switches to the results dashboard: subscriber outcome donut, establishment curve, latency histogram, threshold pass/fail table, and artifact download links.

---

## Production deployment

### Single-binary deployment

Ship two binaries: `bin/radstorm` and `bin/radstorm-api`. No other files required on the production test host for CLI-only runs.

```bash
# On the build machine
make build
scp bin/radstorm operator@testhost:/usr/local/bin/radstorm
scp bin/radstorm-api operator@testhost:/usr/local/bin/radstorm-api
```

### API server environment variables

| Variable | Default | Description |
|---|---|---|
| `RADSTORM_API_ADDR` | `:8080` | Listen address for the API server |
| `RADSTORM_DATA_DIR` | `./data` | Directory for SQLite run store and run output directories |
| `RADSTORM_CLI_BIN` | Auto-resolved | Path to the `radstorm` CLI binary |

The API server auto-discovers the CLI binary by checking (in order): `RADSTORM_CLI_BIN`, `./bin/radstorm`, `radstorm` in PATH. If none are found, the server starts but `POST /runs` returns `503`.

### Reverse proxy considerations

The API server has no built-in TLS. If the frontend is served from a different origin, the API server allows CORS from `http://localhost:3000` by default. For production, either:
- Serve both API and frontend from the same origin (nginx proxy `/api` to `:8080`, `/` to the Next.js build), or
- Set `RADSTORM_CORS_ORIGINS` to your frontend's origin.

The frontend expects the API at `http://localhost:8080` by default. Set `NEXT_PUBLIC_API_BASE_URL` in the Next.js environment if the API is at a different address.

### Data persistence

The API server persists run metadata in SQLite at `$RADSTORM_DATA_DIR/runs.db`. Run output files live under `$RADSTORM_DATA_DIR/runs/<run-id>/`. The SQLite file uses WAL mode and is safe to back up with `VACUUM INTO` or by stopping the server.

On restart, any runs in `queued`, `running`, or `cancelling` state are automatically transitioned to `failed` with reason `orphaned (server restart)`. Subsequent `GET /runs/{id}` calls will reflect this.

---

## Cancellation

### CLI

Press `Ctrl-C` while a run is in progress. The CLI catches `SIGINT`/`SIGTERM`, stops activating new subscribers, waits up to the configured drain period for in-flight requests to complete, flushes the collector, and writes `summary.json` with `outcome=cancelled`. Exit code is `0`.

To configure drain duration:

```bash
./bin/radstorm run-scenario --config scenario.toml --out results/ --drain-seconds 30
```

### API

```bash
curl -X POST http://localhost:8080/api/v1/runs/<run-id>/cancel
```

The run transitions to `cancelling`, the CLI subprocess receives `SIGTERM`, drains, and the run transitions to `cancelled`. Poll `GET /api/v1/runs/{id}` or watch the SSE stream for the final status.

---

## Output artifacts

Every run writes the following files to `<output.directory>/<run-id>/`:

| File | Format | Contents |
|---|---|---|
| `summary.json` | JSON | Full run summary (see [Interpreting summary.json](#interpreting-summaryjson)) |
| `summary.txt` | Plain text | Human-readable formatted version of summary.json; produced by `analyze-results` |
| `events-shard-*.parquet` | Parquet (snappy) | Per-event log; one shard file per CPU core. One row per packet sent/received, per state change, per CoA/Disconnect. Used for deep-dive analysis. |
| `subscribers.parquet` | Parquet (snappy) | Per-subscriber outcome table. One row per virtual subscriber with final state, establishment latency, retransmit counts. |
| `progress.jsonl` | JSONL | Per-second progress snapshots. Consumed by the API server's SSE stream. |
| `run.log` | JSON lines (slog) | Operational log of the harness itself. Use this to diagnose harness issues, not subscriber outcomes. |
| `config.toml` | TOML | Frozen copy of the input config. Always present, even on cancelled runs. |

### Reading Parquet files

```bash
# With DuckDB (recommended for quick queries)
duckdb -c "SELECT final_state, count(*) FROM 'results/first/subscribers.parquet' GROUP BY 1"

# With Python
python -c "import pyarrow.parquet as pq; t = pq.read_table('results/first/subscribers.parquet'); print(t.to_pandas().describe())"
```

### Analyzing results from the CLI

```bash
./bin/radstorm analyze-results results/first/

# Pretty-print summary.json
./bin/radstorm analyze-results results/first/ --json | jq .
```
