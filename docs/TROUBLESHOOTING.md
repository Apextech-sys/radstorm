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

---

## Production failure modes

The following issues appear only at scale (10k+ subscribers on a dedicated Linux host). They do not arise in the laptop Docker rig environment.

---

### Identifier exhaustion at scale

**Symptom:** `run.log` contains `"msg":"identifier exhausted"`. Subscribers stall; `established` count stops rising while `in_flight` keeps climbing.

**Capacity math:** Each `(srcIP, srcPort, dstIP, dstPort)` 4-tuple supports 256 concurrent in-flight requests. With one source IP and port range [10000, 60000]:

```
50,000 ports × 256 IDs = 12.8M maximum — but this is theoretical.
At 1M subscribers with 10% concurrency: 100,000 in-flight / 256 = ~391 pairs needed.
At pessimal (all in-flight): 1,000,000 / 256 = ~3,907 pairs → 8 IPs × 500 ports each.
```

**Fix:** Add source IP aliases. Each new source IP adds `port_range_width × 256` capacity.

```bash
# Add 8 aliases on eth0
for i in $(seq 1 8); do
    sudo ip addr add 192.168.100.${i}/24 dev eth0 label eth0:${i}
done

# Verify all bound
ip addr show eth0 | grep 192.168.100
```

Update `source.ips` in your config with all new addresses.

---

### Source IP alias not actually bound

**Symptom:** `validate-config` fails with `source.ips: bind failed on 192.168.100.2` or the run starts but immediately produces `ErrIdentifierExhausted` even though you think you have enough IPs.

**Cause:** IP aliases do not survive a reboot unless made persistent.

**Verify:**

```bash
ip addr show eth0 | grep 192.168.100
# Each alias you expect should appear with "secondary" label
```

If an alias is missing, add it again:

```bash
sudo ip addr add 192.168.100.2/24 dev eth0 label eth0:2
```

For persistent aliases, use Netplan (Ubuntu) or `ifcfg` files (RHEL). See [docs/PRODUCTION-DEPLOYMENT.md §5](PRODUCTION-DEPLOYMENT.md#5-source-ip-alias-setup).

---

### Port range exhaustion

**Symptom:** `run.log` or `dmesg` shows socket bind failures. The OS cannot allocate source ports within the configured range.

**Cause:** The sysctl `net.ipv4.ip_local_port_range` is narrower than your `source.port_range`, or the OS has consumed ports in that range for other connections.

**Detect:**

```bash
sysctl net.ipv4.ip_local_port_range
# Default output: net.ipv4.ip_local_port_range = 32768	60999
# This overlaps with radstorm's default port_range [10000, 60000] — potential conflict
```

**Fix:** Widen the dedicated port range and ensure it does not overlap with the OS ephemeral range:

```bash
# Push OS ephemeral range above 60000
sudo sysctl -w net.ipv4.ip_local_port_range="60001 65000"

# Now radstorm can safely use [10000, 59999]
# Update source.port_range in your scenario config accordingly
```

Make persistent:

```bash
echo "net.ipv4.ip_local_port_range = 60001 65000" | sudo tee -a /etc/sysctl.d/99-radstorm.conf
sudo sysctl -p /etc/sysctl.d/99-radstorm.conf
```

---

### Kernel UDP buffer drops

**Symptom:** Run completes but `established` count is lower than expected. `run.log` shows no timeouts, but `retransmits.total` is high. The server is fast but replies are being lost on the receive path.

**Detect:**

```bash
netstat -su | grep -i 'receive buffer errors\|errors'
# Or
cat /proc/net/udp | awk '{print $8}' | sort | uniq -c
# Check /proc/net/snmp for RcvbufErrors
cat /proc/net/snmp | grep Udp
# InErrors column should be near zero
```

If `RcvbufErrors` is climbing during the run, the kernel is dropping UDP packets because the receive buffer is full.

**Fix:**

```bash
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.rmem_default=33554432

# Make persistent
echo "net.core.rmem_max = 134217728" | sudo tee -a /etc/sysctl.d/99-radstorm.conf
echo "net.core.rmem_default = 33554432" | sudo tee -a /etc/sysctl.d/99-radstorm.conf
sudo sysctl -p /etc/sysctl.d/99-radstorm.conf
```

---

### File descriptor limits

**Symptom:** The run process crashes with `too many open files`, or `validate-config` fails with `bind: too many open files`.

**Detect:**

```bash
ulimit -n
# If this is 1024, it is too low for large-scale tests
```

**Fix (current session):**

```bash
ulimit -n 1048576
```

**Fix (permanent):**

```bash
cat <<'EOF' | sudo tee -a /etc/security/limits.conf
*    soft    nofile    1048576
*    hard    nofile    1048576
EOF
# Log out and back in, or restart the service
```

For systemd services, add `LimitNOFILE=1048576` to the `[Service]` block. See [docs/PRODUCTION-DEPLOYMENT.md §7](PRODUCTION-DEPLOYMENT.md#7-file-descriptor-limits).

---

### Conntrack table fills (firewall in path)

**Symptom:** After tens of thousands of subscribers, new packets stop going through. The RADIUS server appears unresponsive. `dmesg` shows `nf_conntrack: table full, dropping packet`.

**Cause:** If iptables or nftables with connection tracking is active on the test host, each UDP flow creates a conntrack entry. At large scale, the conntrack table fills.

**Detect:**

```bash
sudo cat /proc/sys/net/netfilter/nf_conntrack_count
sudo cat /proc/sys/net/netfilter/nf_conntrack_max
# If count is close to max, the table is filling
```

**Fix — increase table size:**

```bash
sudo sysctl -w net.netfilter.nf_conntrack_max=2000000
echo "net.netfilter.nf_conntrack_max = 2000000" | sudo tee -a /etc/sysctl.d/99-radstorm.conf
```

**Fix — disable conntrack for RADIUS flows entirely (preferred at scale):**

```bash
# Skip conntrack for outbound RADIUS packets
sudo iptables -t raw -A OUTPUT -p udp --dport 1812 -j NOTRACK
sudo iptables -t raw -A OUTPUT -p udp --dport 1813 -j NOTRACK
# Skip conntrack for inbound replies
sudo iptables -t raw -A PREROUTING -p udp --sport 1812 -j NOTRACK
sudo iptables -t raw -A PREROUTING -p udp --sport 1813 -j NOTRACK
```

---

### Server replies arrive but with bad Response Authenticator

**Symptom:** `run.log` contains `"msg":"validation_failed"` or `"bad authenticator"` for many packets. Subscribers keep retransmitting and eventually time out, but the RADIUS server is sending replies.

**Cause:** The RADIUS server sent a reply using a different shared secret than radstorm used. This can happen when the RADIUS server is a load-balanced cluster where different nodes have different NAS entries with different secrets, or when the NAS record on the server has a stale secret.

**Fix:** Ensure `[target].shared_secret` matches the NAS secret for radstorm's source IP on every node behind the load balancer. If the server cluster uses per-NAS secrets, all nodes must agree on the same secret for radstorm's `nas.ip_address`.

To verify, capture one packet exchange with `tcpdump` and manually verify the Response Authenticator:

```bash
sudo tcpdump -i eth0 -w /tmp/radius-capture.pcap udp port 1812
# Run a single-auth test
./bin/radstorm single-auth --config /data/scenarios/test.toml --user sub00000001 --pass pw00000001
# Ctrl-C the tcpdump
# Open the pcap in Wireshark and check the RADIUS → Response Authenticator field
```

---

### Test box CPU saturated

**Symptom:** Run takes much longer than expected. `top` or `htop` shows the radstorm process at 100% on all cores. The RADIUS server's CPU is low.

**Cause:** The sharded collector, socket pool, or Parquet writers are CPU-bound on the test host. This is a test infrastructure bottleneck, not a RADIUS server issue.

**Detect:**

```bash
top -p $(pgrep radstorm) -d 1
# Watch %CPU column; if near 100 × core_count, radstorm is the bottleneck
```

**Fix:**
1. Run on a host with more CPU cores (the collector scales linearly with cores)
2. Increase `output.flush_interval_sec` to reduce Parquet write pressure (at the cost of higher memory usage)
3. Reduce subscriber count or use `cold_start` (Gaussian ramp) instead of `pessimal` to spread the load over time
4. Ensure the test host has no other significant processes running

---

### Scenario completes but `still_in_flight_at_end > 0`

**Symptom:** The run finishes with `outcome=succeeded` but `summary.json` shows `"still_in_flight_at_end": N` where N > 0.

**Cause:** The drain timeout (`--drain-seconds`) expired before all in-flight requests received replies. These subscribers were abandoned — they are not counted in `established` or `auth_failed`, they are simply discarded.

**Fix:** Increase the drain timeout:

```bash
./bin/radstorm run-scenario \
  --config /data/scenarios/scenario.toml \
  --out /data/results/run1 \
  --drain-seconds 120    # increase from default 30
```

The drain timeout should be at least `retransmit.initial_timeout_ms × (retransmit.max_retries + 1)` = `5000 × 4 = 20,000ms = 20 seconds` for the default policy. Use 60–120 seconds as a safe value for large runs where some subscribers may be in their final retransmit cycle when the ramp ends.
