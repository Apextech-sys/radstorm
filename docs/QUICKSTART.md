# radstorm Quickstart

<!-- Purpose: 5-minute guide from a fresh clone to a successful 100-subscriber run against the local Docker FreeRADIUS rig. -->

Get from zero to a completed RADIUS stress test in five minutes. Assumes Docker Desktop is installed and running.

---

## Step 1 — Clone and build

```bash
git clone https://github.com/Apextech-sys/reflex-radstorm.git
cd reflex-radstorm

# Build the CLI and API binaries
make build
```

Expected output ends with:

```
go build -o bin/radstorm ./apps/cli/cmd/radstorm
go build -o bin/radstorm-api ./apps/api/cmd/radstorm-api
```

---

## Step 2 — Start the Docker FreeRADIUS rig

```bash
make docker-up
```

This starts a Dockerized FreeRADIUS server with 5000 pre-seeded test users. Wait for it to become healthy:

```bash
docker ps | grep freeradius
# Should show: (healthy)
```

The rig serves on:
- Auth: `127.0.0.1:11812`
- Accounting: `127.0.0.1:11813`
- Shared secret: `testing123`

---

## Step 3 — Run the smoke scenario

```bash
./bin/radstorm run-scenario \
  --config test/fixtures/scenarios/smoke-100.toml \
  --out results/smoke-100
```

The CLI prints a progress line every second and a summary line on completion:

```
run run-1777705832 — outcome=succeeded subscribers=100/100 duration=5668ms
artifacts: results/smoke-100
```

---

## Step 4 — Inspect the results

```bash
# Quick text summary
./bin/radstorm analyze-results results/smoke-100

# Full JSON
jq . results/smoke-100/summary.json
```

Key fields to check:

```json
{
  "outcome": "succeeded",
  "subscribers": { "established": 100, "auth_failed": 0 },
  "establishment": { "latency_ms": { "p50": 10, "p99": 35 } }
}
```

All 100 subscribers should be established with sub-50ms p99 latency against the local Docker rig.

---

## Step 5 (optional) — Try the web UI

```bash
# In one terminal: start the API + frontend
make dev
```

Open `http://localhost:3000`. Navigate to **New Run**, pick the "Smoke 100" template, click **Start Run**. Watch the live establishment curve and stat cards. Results appear automatically when the run completes.

---

## Next steps

- **More subscribers**: edit the `[subscribers]` section of a TOML file and point it at a larger credentials file
- **Different scenarios**: change `scenario.type` to `cold_start`, `uniform`, or `pessimal`
- **Real RADIUS server**: update `[target]` with your server's address and shared secret
- **Scale to thousands**: read the [Source IP setup](RUNBOOK.md#source-ip-setup) section in the Runbook
- **Full operator reference**: [docs/RUNBOOK.md](RUNBOOK.md)
- **Configuration schema**: [docs/CONFIG.md](CONFIG.md)

---

## Troubleshooting quick-reference

| Symptom | Fix |
|---|---|
| `validate-config fails: credentials_file: file not found` | Use an absolute path or run from repo root |
| `RADIUS server times out` | Check shared secret matches; Docker rig ports are 11812/11813, not 1812/1813 |
| Docker rig not healthy after 60s | `docker logs radstorm-freeradius` to diagnose |
| `ErrIdentifierExhausted` | Add more source IPs; see [RUNBOOK.md](RUNBOOK.md#source-ip-setup) |

Full troubleshooting guide: [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md)

---

**Next: real evaluation work** → [docs/EVALUATION-GUIDE.md](EVALUATION-GUIDE.md)
