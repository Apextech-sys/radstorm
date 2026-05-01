# Briefing 1E — Docker FreeRADIUS test rig

## Mission

Build a Dockerized FreeRADIUS instance preconfigured for radstorm's local E2E tests. Must be one `docker compose up` away from a working RADIUS server with seeded test users and the right shared secrets. Plus a smoke script that proves it works using `radclient`.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/CONVENTIONS.md`
3. `docs/ARCHITECTURE.md` (the "Local dev rig" section)
4. `docs/PROTOCOL.md`
5. `.orchestration/contracts/config-schema.md` (need to know what addresses/secrets the radstorm config will use against this rig)

## Working directory

- **Worktree:** `C:\dev\radstorm` on `main` (no parallel writers to `test/docker/`)
- **Files you own:**
  - `test/docker/docker-compose.yml`
  - `test/docker/freeradius/clients.conf`
  - `test/docker/freeradius/users` (or equivalent SQL backend if you prefer)
  - `test/docker/freeradius/sites-enabled/default`
  - `test/docker/freeradius/radiusd.conf` only if defaults need overrides
  - `test/docker/freeradius/Dockerfile` (custom image based on `freeradius/freeradius-server:latest`)
  - `test/docker/seed/generate-users.sh` — generates a flat `users` file from the credentials CSV that radstorm uses
  - `test/docker/smoke.sh` — runs `radclient` from a sidecar container against the FreeRADIUS to prove auth works
  - `test/docker/README.md` — operator instructions
  - `test/fixtures/credentials/smoke-100.csv` — 100 test creds (sub00000001 / pw00000001 ...)
  - `test/fixtures/credentials/test-1k.csv` — 1000 test creds
  - `test/fixtures/scenarios/smoke-100.toml` — radstorm scenario config pointing at the rig
  - `test/fixtures/scenarios/test-1k.toml`

## Scope

**In scope:**
- FreeRADIUS image: `freeradius/freeradius-server:3.2-alpine` (3.2 is the current stable; 4.x is still beta)
- Expose UDP 1812 (auth) and UDP 1813 (acct) on host. Use non-default host ports if convenient (`1812 → 11812` etc.) but the scenario fixtures must match
- `clients.conf`: allow `0.0.0.0/0` from inside the docker network with shared secret `testing123`. Also allow host `127.0.0.1` and `host.docker.internal` so the host running radstorm can connect.
- Users file: 5000 seeded test users (`sub00000001` / `pw00000001` ... `sub00005000` / `pw00005000`), all with `Cleartext-Password` so both PAP and CHAP work. Generate this from the seed script so it's reproducible.
- Site config that handles auth + accounting and immediately returns Access-Accept / Accounting-Response
- Container restarts cleanly; logs go to stdout
- `smoke.sh`: runs `docker compose run --rm radclient` with a one-shot Access-Request for `sub00000001/pw00000001` and asserts Access-Accept; same for an Accounting-Request
- Credentials CSV files with the format from `.orchestration/contracts/config-schema.md`
- Scenario fixture TOMLs that point at `127.0.0.1:1812` (or whichever host port you map to)
- README explaining how to up/down/smoke

**Out of scope:**
- Production FreeRADIUS tuning (this is a local test rig, default tuning is fine)
- Authoritative dictionary for Huawei (use the dictionary that ships with the FreeRADIUS image; if Huawei.dict is missing, mount one from `pkg/radius/dictionaries/huawei.dict` after slice 1A creates it — coordinate via brief comment in compose file)

## Success criteria

- `docker compose -f test/docker/docker-compose.yml up -d` brings up FreeRADIUS in <30s on a Windows Docker Desktop host
- `bash test/docker/smoke.sh` returns success and prints "Access-Accept" + "Accounting-Response"
- `docker compose -f test/docker/docker-compose.yml down -v` cleans up fully
- `make docker-up` and `make docker-down` work via the Makefile

## Test requirements

- The smoke script IS the test. Make it bash + `radclient`, no extra runtimes needed.
- If you can run the smoke script in CI under `docker compose`, even better

## File-header requirement

Header on every script and every YAML/conf file you author. Per `docs/CONVENTIONS.md`.

## Reporting

Write `.orchestration/reports/1e-docker-rig.md`:
- Image used, ports exposed, secret used
- Steps to run smoke
- Output of the smoke run (paste it)
- Any gotchas Windows users need to know
