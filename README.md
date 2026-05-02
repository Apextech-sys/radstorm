# radstorm

[![CI](https://github.com/Apextech-sys/reflex-radstorm/actions/workflows/ci.yml/badge.svg)](https://github.com/Apextech-sys/reflex-radstorm/actions/workflows/ci.yml)

radstorm is a protocol-level RADIUS stress tester for ISP-scale subscriber environments. It simulates up to 1M virtual PPPoE/MAC subscribers, drives full authentication and accounting flows against a target RADIUS server, and produces sharded Parquet event logs plus a structured `summary.json` for analysis.

Validated end-to-end: 100 subscribers in under 6 seconds against Dockerized FreeRADIUS, with p50=10ms and p99=35ms establishment latency, sharded Parquet output, and a live frontend dashboard.

---

## First-time user

**[Start here: docs/QUICKSTART.md](docs/QUICKSTART.md)**

Five minutes from clone to first successful run. Assumes Docker Desktop is installed.

---

## Architecture

```
┌─────────────────────┐   REST/SSE   ┌──────────────────────┐  spawn  ┌──────────────────────┐
│  Next.js frontend    │◀────────────▶│  Go HTTP API server   │────────▶│  Go CLI (radstorm)    │
│  (apps/web)          │              │  (apps/api)           │         │  (apps/cli)           │
│  - config form       │              │  - run lifecycle       │         │  - scenario driver    │
│  - live progress     │              │  - SSE progress stream │         │  - subscriber pool    │
│  - results dashboard │              │  - SQLite run store    │         │  - UDP I/O + collector│
└─────────────────────┘              └──────────────────────┘         └──────────┬───────────┘
                                                                                  │ UDP RADIUS
                                                                                  ▼
                                                                       ┌──────────────────────┐
                                                                       │  RADIUS server under  │
                                                                       │  test (FreeRADIUS,    │
                                                                       │  Interstellar, etc.)  │
                                                                       └──────────────────────┘
```

The CLI is the test engine. The API server is a thin process supervisor. The frontend is configuration and visualization. The CLI runs standalone — no API or frontend required.

---

## Component map

| Component | Location | Description |
|---|---|---|
| CLI | `apps/cli/cmd/radstorm` | `run-scenario`, `validate-config`, `single-auth`, `analyze-results` subcommands |
| API server | `apps/api/cmd/radstorm-api` | REST + SSE wrapper around the CLI; SQLite run store |
| Frontend | `apps/web` | Next.js config form, live run progress, results dashboard |
| `pkg/radius` | `pkg/radius` | Hand-rolled RFC 2865/2866/5176 packet layer; Huawei VSA support |
| `pkg/config` | `pkg/config` | TOML loader, validator, credentials CSV parser |
| `pkg/io` | `pkg/io` | UDP socket pool, 8-bit ID bitmap allocator, reply matcher, sender with retransmit |
| `pkg/subscriber` | `pkg/subscriber` | Per-subscriber FSM (`idle → auth_sent → established → terminated`) |
| `pkg/server` | `pkg/server` | CoA/Disconnect UDP listener (RFC 5176); ACK/NAK with latency recording |
| `pkg/scenario` | `pkg/scenario` | Scenario driver: Gaussian/uniform/pessimal activation schedules |
| `pkg/collector` | `pkg/collector` | Sharded event channels, Parquet flush, summary aggregation |
| `pkg/events` | `pkg/events` | Shared event struct + Parquet schema |

---

## Quick start

```bash
# Clone and build
git clone https://github.com/Apextech-sys/reflex-radstorm.git
cd reflex-radstorm
make build

# Start the Docker FreeRADIUS rig
make docker-up

# Run 100 subscribers
./bin/radstorm run-scenario \
  --config test/fixtures/scenarios/smoke-100.toml \
  --out results/smoke-100

# Inspect results
./bin/radstorm analyze-results results/smoke-100
jq '.establishment.latency_ms' results/smoke-100/summary.json
```

Or with the web UI:

```bash
make dev       # starts API on :8080 and Next.js on :3000
# Open http://localhost:3000 → New Run
```

---

## Running the E2E suite

The E2E suite runs the full stack against the Docker FreeRADIUS rig and asserts outcome, latency, and Parquet output.

```bash
# Runs: make docker-up, make build, bash test/e2e/run.sh, make docker-down
make e2e
```

Exit codes: `0` = all pass, `1` = assertion failures, `2` = binary not built, `3` = prereqs missing.

See [`test/e2e/README.md`](test/e2e/README.md) for individual scenario execution and fixture details.

---

## Repo layout

```
/
  apps/
    cli/             CLI entry point (radstorm binary)
    api/             HTTP API server (radstorm-api binary)
    web/             Next.js frontend
  pkg/
    radius/          RADIUS protocol layer
    config/          Configuration loading + validation
    io/              UDP I/O: socket pool, ID allocator, sender, receiver
    subscriber/      Subscriber FSM and pool
    server/          CoA/Disconnect listener
    scenario/        Scenario driver and activation schedules
    collector/       Event collection and Parquet output
    events/          Shared event types
  test/
    docker/          Docker FreeRADIUS test rig
    e2e/             End-to-end test scripts
    fixtures/        Scenario configs and credential CSVs
  docs/
    ARCHITECTURE.md  System architecture
    PROTOCOL.md      RADIUS protocol reference
    CONFIG.md        Configuration schema reference
    RUNBOOK.md       Operator runbook
    QUICKSTART.md    5-minute getting-started guide
    TROUBLESHOOTING.md  Common issues and fixes
    CONVENTIONS.md   Code and documentation standards
    decisions/       Architecture Decision Records
  .github/
    workflows/       CI pipeline (Go tests, frontend tests, E2E)
  .orchestration/    Build orchestration state (internal)
  Makefile
```

---

## Documentation

| Document | Purpose |
|---|---|
| [docs/QUICKSTART.md](docs/QUICKSTART.md) | 5-minute getting-started guide |
| [docs/RUNBOOK.md](docs/RUNBOOK.md) | Full operator runbook |
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | Common issues and fixes |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System architecture |
| [docs/PROTOCOL.md](docs/PROTOCOL.md) | RADIUS protocol reference |
| [docs/CONFIG.md](docs/CONFIG.md) | Configuration schema |
| [docs/CONVENTIONS.md](docs/CONVENTIONS.md) | Code and documentation conventions |
| [docs/decisions/](docs/decisions/) | Architecture Decision Records |

---

## Project conventions

Every code file in this repo begins with a header comment block documenting its purpose, related files, briefing reference, and contract ownership. See [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md).

The header convention exists so any contributor — human or agent — can understand any file in isolation.

---

## License

Proprietary — internal use only.
