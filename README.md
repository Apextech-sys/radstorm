# radstorm — RADIUS Stress Tester

**Status:** Under active development. See [`.orchestration/STATE.md`](.orchestration/STATE.md) for current build state.

A protocol-level stress test harness for RADIUS servers serving ISP scale (~1M PPPoE/MAC subscribers). Built to validate FreeRADIUS and Interstellar candidates against realistic outage-recovery and CoA-storm scenarios.

## What it does

Simulates large numbers of virtual subscribers, each independently progressing through full RADIUS authentication + accounting establishment against a target RADIUS server. Records per-subscriber timing and outcome data, plus a server-initiated CoA/Disconnect listener for change-of-authorization storm testing.

## Components

- **`apps/cli/`** — Go CLI binary (`radstorm`) — runs scenarios, emits Parquet/JSON results
- **`apps/api/`** — Go HTTP API server (`radstorm-api`) — wraps the CLI for the frontend
- **`apps/web/`** — Next.js frontend — scenario configuration, run trigger, results dashboard
- **`pkg/`** — Shared Go packages (radius, config, subscriber, io, scenario, collector, server)
- **`test/docker/freeradius/`** — Dockerized FreeRADIUS test rig for local end-to-end validation

## Quick start

```bash
# Bring up the local FreeRADIUS test rig
docker compose -f test/docker/docker-compose.yml up -d

# Build the CLI and API
make build

# Run a small smoke test (1k subscribers against the local FreeRADIUS)
./bin/radstorm run-scenario --config test/fixtures/scenarios/smoke-1k.toml

# Or via the web UI
make dev   # starts API + Next.js dev server
# Open http://localhost:3000
```

## Documentation

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — system architecture
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md) — RADIUS protocol reference, Huawei VSA notes
- [`docs/CONFIG.md`](docs/CONFIG.md) — scenario configuration schema
- [`docs/RUNBOOK.md`](docs/RUNBOOK.md) — operator runbook
- [`docs/decisions/`](docs/decisions/) — Architecture Decision Records

## Project conventions

Every code file in this repo MUST start with a header comment block that explains:
- **Purpose:** what this file does in 1–2 lines
- **Related files:** other files a reader needs to understand context
- **Briefing:** path to the briefing doc that drove this file's creation
- **Contract:** what externally-visible contracts this file owns (API surface, schema, etc.)

This convention exists so any future contributor (human or agent) can understand any file in isolation. See [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md).

## License

Proprietary — internal use only.
