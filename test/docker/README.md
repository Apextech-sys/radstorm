# radstorm Docker FreeRADIUS Test Rig

<!-- Briefing: .orchestration/briefings/1e-docker-rig.md -->

Local end-to-end test target for radstorm development. One `docker compose up` brings up a FreeRADIUS 3.2 instance pre-loaded with 5,000 seeded test users.

## Quick reference

```bash
# Bring up FreeRADIUS (detached)
docker compose -f test/docker/docker-compose.yml up -d

# Or via Makefile shortcut
make docker-up

# Smoke test (proves auth + accounting work)
bash test/docker/smoke.sh

# Tear down + remove volumes
docker compose -f test/docker/docker-compose.yml down -v

# Or via Makefile
make docker-down
```

## Network topology

| Service | Host port | Container port | Protocol |
|---|---|---|---|
| FreeRADIUS auth | `11812` | `1812` | UDP |
| FreeRADIUS acct | `11813` | `1813` | UDP |

Non-default host ports (`11812`/`11813`) are used deliberately to avoid requiring elevated privileges on Windows Docker Desktop. The scenario TOML fixtures in `test/fixtures/scenarios/` already reference these ports.

## Shared secret

`testing123` — applies to all clients. Matches `test/docker/freeradius/clients.conf`.

## Seeded test users

5,000 users pre-loaded in FreeRADIUS flat-file format:

| Username | Password |
|---|---|
| `sub00000001` | `pw00000001` |
| `sub00000002` | `pw00000002` |
| ... | ... |
| `sub00005000` | `pw00005000` |

All users have `Cleartext-Password` set, which means both PAP and CHAP authentication work out of the box.

To regenerate the `users` file and CSV fixtures:

```bash
bash test/docker/seed/generate-users.sh
```

## Credential CSV fixtures

| File | Users | Purpose |
|---|---|---|
| `test/fixtures/credentials/smoke-100.csv` | 100 | Quick smoke / CI |
| `test/fixtures/credentials/test-1k.csv` | 1,000 | Integration test |

## Scenario TOML fixtures

| File | Subscribers | Scenario type |
|---|---|---|
| `test/fixtures/scenarios/smoke-100.toml` | 100 | `uniform` (10s ramp) |
| `test/fixtures/scenarios/test-1k.toml` | 1,000 | `cold_start` (Gaussian) |

## Windows Docker Desktop gotchas

- **Port mapping**: Docker Desktop on Windows maps container ports via a lightweight VM. UDP ports work, but you may need to ensure no Windows Firewall rule blocks `11812`/`11813`.
- **Health check**: The container health check sends a real Access-Request internally. If `docker compose up --wait` times out, check `docker logs radstorm-freeradius` for startup errors.
- **Line endings**: The `users` file is generated with Unix LF endings. FreeRADIUS on Alpine expects LF; do not let your editor convert to CRLF.

## File layout

```
test/docker/
  docker-compose.yml          — compose definition
  smoke.sh                    — smoke test script
  README.md                   — this file
  freeradius/
    Dockerfile                — custom image (bakes in config)
    clients.conf              — NAS client authorisation
    users                     — flat-file user DB (auto-generated)
    sites-enabled/
      default                 — virtual server config
  seed/
    generate-users.sh         — regenerate users + CSV fixtures
test/fixtures/
  credentials/
    smoke-100.csv             — 100 test credentials
    test-1k.csv               — 1000 test credentials
  scenarios/
    smoke-100.toml            — 100-sub smoke scenario
    test-1k.toml              — 1k-sub integration scenario
```
