# Report: 1E — Docker FreeRADIUS Test Rig

**Agent:** 1E (devops-engineer)
**Branch:** wave-1/1e-docker-rig
**Date:** 2026-05-02
**Status:** COMPLETE — smoke test passes

---

## Summary

Built and validated a Dockerized FreeRADIUS 3.2 test rig for radstorm local end-to-end testing. FreeRADIUS is up in ~7 seconds, and the smoke test confirms both Access-Accept (PAP auth) and Accounting-Response (Acct-Start) from seeded user `sub00000001`.

---

## Configuration

| Parameter | Value |
|---|---|
| Base image | `freeradius/freeradius-server:latest` (FreeRADIUS 3.2.8, Ubuntu 22.04) |
| Host auth port | `11812` → container `1812/udp` |
| Host acct port | `11813` → container `1813/udp` |
| Shared secret | `testing123` |
| Seeded users | 5,000 (`sub00000001/pw00000001` ... `sub00005000/pw00005000`) |
| Auth methods | PAP + CHAP (Cleartext-Password works for both) |
| Config root | `/etc/freeradius/` |

**Image note:** The briefing spec requested `freeradius/freeradius-server:3.2-alpine` but that tag does not exist on Docker Hub as of 2026-05-02. The `latest` tag ships FreeRADIUS 3.2.8 (the same version) on Ubuntu 22.04. All config paths and behaviour are identical.

---

## Files created

| File | Purpose |
|---|---|
| `test/docker/docker-compose.yml` | Compose definition; health check; port mapping |
| `test/docker/freeradius/Dockerfile` | Custom image overlaying clients.conf + users |
| `test/docker/freeradius/clients.conf` | NAS client authorisation (0.0.0.0/0 + localhost) |
| `test/docker/freeradius/users` | Flat-file user DB, 5000 entries (auto-generated) |
| `test/docker/seed/generate-users.sh` | Regeneration script for users + CSV fixtures |
| `test/docker/smoke.sh` | Smoke test script (radclient via Docker sidecar) |
| `test/docker/README.md` | Operator instructions |
| `test/fixtures/credentials/smoke-100.csv` | 100-user credential CSV |
| `test/fixtures/credentials/test-1k.csv` | 1000-user credential CSV |
| `test/fixtures/scenarios/smoke-100.toml` | 100-sub uniform scenario TOML |
| `test/fixtures/scenarios/test-1k.toml` | 1k-sub cold_start scenario TOML |

---

## How to use

```bash
# Bring up FreeRADIUS
docker compose -f test/docker/docker-compose.yml up -d

# Run smoke test
bash test/docker/smoke.sh

# Tear down
docker compose -f test/docker/docker-compose.yml down -v

# Via Makefile shortcuts
make docker-up
make docker-down
```

---

## Smoke test output

```
[INFO] FreeRADIUS network: docker_radius_net
[INFO] Test 1: Sending Access-Request (PAP) for sub00000001...
Sent Access-Request Id 55 from 0.0.0.0:36263 to 172.24.0.2:1812 length 97
	Message-Authenticator = 0x
	User-Name = "sub00000001"
	User-Password = "pw00000001"
	NAS-IP-Address = 127.0.0.1
	NAS-Identifier = "radstorm-smoke"
	Service-Type = Framed-User
	Cleartext-Password = "pw00000001"
Received Access-Accept Id 55 from 172.24.0.2:1812 to 172.24.0.3:36263 length 38
	Message-Authenticator = 0xc7d378721f2520ac7b254e7e7c729fea

[PASS] Access-Accept received
[INFO] Test 2: Sending Accounting-Request (Start) for sub00000001...
Sent Accounting-Request Id 109 from 0.0.0.0:54762 to 172.24.0.2:1813 length 91
	User-Name = "sub00000001"
	NAS-IP-Address = 127.0.0.1
	NAS-Identifier = "radstorm-smoke"
	Acct-Status-Type = Start
	Acct-Session-Id = "smoke-1777690533"
	Acct-Authentic = RADIUS
	NAS-Port = 1
Received Accounting-Response Id 109 from 172.24.0.2:1813 to 172.24.0.3:54762 length 20

[PASS] Accounting-Response received

============================================
 Smoke test PASSED -- FreeRADIUS rig is OK
============================================

  Auth  : sub00000001 @ radstorm-freeradius:1812 -> Access-Accept
  Acct  : sub00000001 @ radstorm-freeradius:1813 -> Accounting-Response
  Secret: testing123
  Image : freeradius/freeradius-server:latest (FreeRADIUS 3.2.x)
```

---

## Acceptance gates met

| Gate | Result |
|---|---|
| `docker compose up -d` starts FreeRADIUS in <30s | PASS (7s) |
| `bash test/docker/smoke.sh` prints Access-Accept | PASS |
| `bash test/docker/smoke.sh` prints Accounting-Response | PASS |
| `docker compose down -v` cleans up fully | PASS |

---

## Windows Docker Desktop gotchas

1. **Non-privileged ports**: Container ports 1812/1813 map to host ports 11812/11813. On Windows, binding host ports below 1024 requires elevation; the non-standard mapping avoids this. The scenario TOML fixtures reference `127.0.0.1:11812` / `127.0.0.1:11813` accordingly.

2. **No Alpine tag**: `freeradius/freeradius-server:3.2-alpine` does not exist on Docker Hub. The `latest` tag (Ubuntu 22.04, FreeRADIUS 3.2.8) is the only published tag and works identically.

3. **Smoke script uses Docker sidecar**: The smoke script runs `radclient` inside a temporary container on the same Docker network. This avoids needing any RADIUS tools installed on the host. On Windows this works correctly as long as Docker Desktop has Linux container mode active.

4. **users file line endings**: The `users` file is generated with LF (Unix) line endings. FreeRADIUS parses this correctly. Do not let an editor convert to CRLF.

5. **Health check format**: `radclient` reads attribute-value pairs from stdin; the health check uses `CMD-SHELL` with a pipe to pass them correctly.
