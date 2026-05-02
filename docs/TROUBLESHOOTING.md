# radstorm Troubleshooting

<!-- Purpose: Common operator problems and their resolutions, organized by symptom. -->

## `validate-config` fails: `credentials_file: file not found`

**Symptom:**

```
validate-config: validation failed: credentials_file: file not found: "test/fixtures/credentials/smoke-100.csv"
```

**Cause:** `credentials_file` is resolved relative to the current working directory when the CLI runs, not relative to the config file's location. Running the CLI from a directory other than the repo root causes the relative path to miss.

**Fix:** Either run the CLI from the repo root:

```bash
# From repo root — path resolves correctly
./bin/radstorm validate-config test/fixtures/scenarios/smoke-100.toml
```

Or use an absolute path in the config:

```toml
[subscribers]
credentials_file = "/home/operator/radstorm/test/fixtures/credentials/smoke-100.csv"
```

---

## RADIUS server times out — all subscribers in `auth_failed`

**Symptom:** Run completes but `subscribers.auth_failed == subscribers.total`. `run.log` contains `"level":"error","msg":"final timeout"` for every subscriber.

**Causes and fixes:**

1. **Wrong port**: The Docker test rig maps FreeRADIUS to host ports `11812` (auth) and `11813` (acct), not the standard `1812`/`1813`. Check your config:

   ```toml
   [target]
   auth_address = "127.0.0.1:11812"
   acct_address = "127.0.0.1:11813"
   ```

2. **Wrong shared secret**: The shared secret in `[target]` must match exactly what FreeRADIUS is configured with. The Docker rig uses `testing123`. A mismatch causes FreeRADIUS to silently drop packets (no response = timeout).

3. **Server not running**: Verify the target server is reachable:

   ```bash
   # For the Docker rig:
   docker ps | grep freeradius
   # Should show (healthy)

   # Quick one-packet test (requires radclient):
   echo "User-Name = sub00000001, User-Password = pw00000001" | \
     radclient 127.0.0.1:11812 auth testing123
   ```

4. **Firewall blocking UDP**: Confirm outbound UDP on the configured port range is allowed:

   ```bash
   sudo ss -u -a | grep 11812
   ```

---

## Docker rig is unhealthy or not starting

**Symptom:** `docker ps` shows `(unhealthy)` or the container is restarting.

**Fix:**

```bash
# Check FreeRADIUS logs
docker logs radstorm-freeradius

# The health check runs every 10 seconds; allow 30 seconds for the full startup
# If still unhealthy after 30s, tear down and restart
make docker-down
make docker-up

# Watch health status
watch -n 2 "docker ps --filter name=radstorm-freeradius --format '{{.Status}}'"
```

Common causes of FreeRADIUS startup failure:

- Port 11812 or 11813 already in use on the host. Check with `sudo ss -ulnp | grep -E '1181[23]'` and stop any conflicting process.
- Docker Desktop not running or out of resources. Restart Docker Desktop.

---

## Identifier exhausted — `ErrIdentifierExhausted`

**Symptom:** `run.log` contains `"msg":"identifier exhausted"`. Some subscribers stall without completing.

**Cause:** All 256 RADIUS Identifiers in every available `(srcIP, srcPort, dstIP, dstPort)` 4-tuple are in use simultaneously. This is the correct error — you have genuinely saturated the protocol's capacity for the current source IP and port configuration.

**Fix (add source IP aliases):**

```bash
# Add aliases on loopback (Linux)
for i in $(seq 2 32); do
    sudo ip addr add 127.0.0.${i}/8 dev lo
done
```

Then update `source.ips` in your config to include all the aliases. See the [Source IP setup](RUNBOOK.md#source-ip-setup) section in the Runbook for capacity math and sysctl tuning.

---

## API server returns `409 Conflict` on `POST /runs`

**Symptom:** Posting a new run via the frontend or curl returns:

```json
{ "error": "a run is already in progress" }
```

**Cause:** The API server enforces single-tenancy — only one run at a time. Another run is in the `queued`, `running`, or `cancelling` state.

**Fix:**

```bash
# List runs to find the active one
curl -s http://localhost:8080/api/v1/runs | jq '.runs[] | select(.status != "succeeded" and .status != "failed" and .status != "cancelled")'

# Cancel it
curl -s -X POST http://localhost:8080/api/v1/runs/<run-id>/cancel

# Wait for cancellation to complete
curl -s http://localhost:8080/api/v1/runs/<run-id> | jq .status
```

If the API server was restarted mid-run, the orphaned run is automatically transitioned to `failed` on the next startup. Restart the API server to clear a stuck run.

---

## Frontend shows "API disconnected" or requests fail with network errors

**Symptom:** The frontend's status indicator shows "API disconnected" or all API calls fail in the browser console.

**Causes and fixes:**

1. **API server not running**: Start it with `make dev` or `./bin/radstorm-api`.

2. **Wrong API address**: The frontend defaults to `http://localhost:8080`. Verify the API is listening:

   ```bash
   curl -s http://localhost:8080/api/v1/health
   # Expected: {"status":"ok"}
   ```

3. **CORS mismatch**: If the frontend is served from a different port or hostname than the API expects, CORS preflight requests will fail. The API defaults to allowing `http://localhost:3000`. If you changed the frontend port, set `RADSTORM_CORS_ORIGINS` on the API server or adjust `NEXT_PUBLIC_API_BASE_URL` in the frontend environment.

4. **API server started without CLI binary**: If `bin/radstorm` is not built, the API starts but `POST /runs` returns `503`. Run `make build-cli` first.

---

## `run.log` shows `bad authenticator` warnings

**Symptom:** `run.log` entries with `"msg":"validation_failed"` or `"msg":"bad authenticator"`.

**Cause:** The RADIUS server sent a reply with an invalid Response Authenticator. This almost always means a shared secret mismatch — the server is using a different secret than the one in `[target].shared_secret`.

**Fix:** Ensure `[target].shared_secret` exactly matches the NAS secret configured in the RADIUS server for radstorm's source IP. Check for trailing spaces or encoding issues.

---

## `go test -race` fails with "cgo required"

**Symptom:**

```
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
```

**Cause:** The race detector requires CGO, which requires a C compiler. On Windows without MSYS2/MinGW, no C compiler is available.

**Fix:** On Linux (CI or WSL), install `gcc` and run normally. On Windows, skip `-race` for local development:

```bash
go test -count=1 ./...   # no -race flag
```

The CI pipeline (`.github/workflows/ci.yml`) runs on `ubuntu-latest` with CGO available and includes `-race`.

---

## Run completes but Parquet files are missing or empty

**Symptom:** `summary.json` is present but `events-shard-*.parquet` or `subscribers.parquet` are absent or zero bytes.

**Cause:** The collector flushes on a 30-second interval. For very short runs (under 30 seconds), the final flush may be the only flush. If the process was killed rather than cancelled (SIGKILL, not SIGTERM), the final flush may not have completed.

**Fix:** Always use Ctrl-C (SIGINT) or `POST /runs/{id}/cancel` rather than killing the process. If the run was killed, re-run the scenario. For runs under 5 seconds, the Parquet flush happens at run end regardless of interval.

---

## `make dev` port conflict — address already in use

**Symptom:** Starting the dev servers fails with `address already in use` on port 8080 or 3000.

**Fix:**

```bash
# Find and kill the process using port 8080
lsof -ti tcp:8080 | xargs kill -9

# For port 3000
lsof -ti tcp:3000 | xargs kill -9

# Then retry
make dev
```

Or change the port via environment variables:

```bash
RADSTORM_API_ADDR=:8081 ./bin/radstorm-api
# Then set NEXT_PUBLIC_API_BASE_URL=http://localhost:8081 for the frontend
```
