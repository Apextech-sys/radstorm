<!-- Purpose: Guide for authoring custom scenario TOML configs from scratch, including field explanations and worked examples. -->

# Custom Scenarios

Audience: operator who has run the smoke-100 fixture and now needs to write a scenario that reflects their actual subscriber population, BNG model, and SLO thresholds.

---

## Table of contents

1. [Anatomy of a scenario TOML](#1-anatomy-of-a-scenario-toml)
2. [Choosing a scenario type](#2-choosing-a-scenario-type)
3. [Sizing cold_start parameters](#3-sizing-cold_start-parameters)
4. [Sizing source IPs and port range](#4-sizing-source-ips-and-port-range)
5. [NAS attributes for your BNG model](#5-nas-attributes-for-your-bng-model)
6. [Custom Huawei VSAs](#6-custom-huawei-vsas)
7. [Threshold customization](#7-threshold-customization)
8. [Validating your config](#8-validating-your-config)
9. [Worked examples](#9-worked-examples)

---

## 1. Anatomy of a scenario TOML

Every field with its operator-level meaning. Required fields are marked **R**.

```toml
# ── Target RADIUS server ──────────────────────────────────────────────────────

[target]
auth_address  = "10.0.0.10:1812"   # R. host:port for Access-Request / Access-Accept
acct_address  = "10.0.0.10:1813"   # R. host:port for Accounting-Request / Accounting-Response
shared_secret = "your-secret"       # R. Must match NAS secret on the RADIUS server. Never logged.


# ── CoA / Disconnect listener ─────────────────────────────────────────────────
# radstorm listens here for server-initiated CoA-Request and Disconnect-Request.
# RFC 5176 default is 3799. Leave as-is unless something else is on that port.

[coa_listener]
bind_address  = "0.0.0.0:3799"    # R. bind address for inbound CoA/Disconnect
shared_secret = "your-secret"       # R. Must match CoA client secret on RADIUS server


# ── Subscriber pool ───────────────────────────────────────────────────────────

[subscribers]
count               = 100000        # R. Number of virtual subscribers to activate. Max: 1,000,000.
credentials_file    = "/data/creds-100k.csv"  # R. Absolute path recommended. Must have >= count rows.
auth_method_pap_pct = 80            # % of subscribers using PAP. Remainder use CHAP. Default: 100.
type_pppoe_pct      = 90            # % of subscribers that are PPPoE. Remainder are MAC-auth. Default: 100.
include_acct_start  = true          # Send Accounting-Request (Start) after auth. Set false for auth-only.


# ── Source IP and port pool ───────────────────────────────────────────────────
# These IPs must be bound on this host before running.
# See docs/PRODUCTION-DEPLOYMENT.md §5 for alias setup.

[source]
ips        = ["192.168.100.1", "192.168.100.2", "192.168.100.3", "192.168.100.4"]
port_range = [10000, 20000]         # [low, high] inclusive. Ports in this range are used as srcPorts.


# ── NAS attributes ────────────────────────────────────────────────────────────
# These go into every Access-Request and Accounting-Request.
# The RADIUS server must recognize nas.ip_address as a valid NAS.

[nas]
ip_address = "10.0.0.1"            # R. NAS-IP-Address (attr 4). Must match a NAS entry on the server.
identifier = "bng-01-eval"          # R. NAS-Identifier (attr 32). Arbitrary string; pick something meaningful.

# Optional Huawei VSAs. See §6 for details.
# [nas.huawei]
# connect_id = "1/1/1.100"


# ── Retransmit policy ─────────────────────────────────────────────────────────
# These mirror what a real NAS would do. Keep defaults for production validation.
# Reduce initial_timeout_ms only when you want to surface slow-server issues faster.

[retransmit]
initial_timeout_ms = 5000           # Wait this long before first retransmit. RFC-typical: 3–5s.
max_retries        = 3              # Maximum retransmit attempts per request. After this: auth_failed.
backoff            = "exponential"  # exponential | linear | constant
backoff_base_ms    = 1000           # With exponential: waits are 1s, 2s, 4s after each retry.


# ── Scenario type and parameters ──────────────────────────────────────────────

[scenario]
type             = "cold_start"    # R. cold_start | uniform | pessimal | coa_storm
hard_timeout_sec = 600             # Abort the entire run if not finished within this many seconds.

# Only one of the following sub-blocks applies, matching the type above.

[scenario.cold_start]
mu_sec         = 120.0              # Centre of the Gaussian ramp, in seconds from run start.
sigma_sec      = 40.0               # Standard deviation. Controls how tight or spread the ramp is.
truncate_sigma = 3.0                # Clamp at mu ± (truncate_sigma × sigma). Avoids infinite tails.

# [scenario.uniform]
# duration_sec = 60.0              # Spread activations evenly over this many seconds.

# [scenario.pessimal]
# burst_window_ms = 0              # 0 = simultaneous. >0 = spread burst over this window in ms.

# [scenario.coa_storm]
# (no parameters; the storm itself is fired externally — see EVALUATION-GUIDE.md §4.5)


# ── Output ────────────────────────────────────────────────────────────────────

[output]
directory         = "/data/results/freeradius/coldstart-100k"  # Created if absent. Use absolute paths.
flush_interval_sec = 30             # How often to flush Parquet write buffers to disk.
                                    # Lower values use more disk I/O but reduce data loss on crash.
```

### Credentials file format

```csv
username,password,auth_method,sub_type,nas_port_id,mac_address
sub00000001,pw00000001,pap,pppoe,1/1/1.100,aa:bb:cc:00:00:01
sub00000002,pw00000002,chap,pppoe,1/1/1.101,aa:bb:cc:00:00:02
sub00000003,pw00000003,pap,mac,1/1/2.100,aa:bb:cc:00:00:03
```

- `auth_method` and `sub_type` columns are optional. If absent, the `auth_method_pap_pct` and `type_pppoe_pct` config values apply.
- `nas_port_id` populates NAS-Port-Id (attr 87).
- `mac_address` populates Calling-Station-Id (attr 31).
- Lines starting with `#` are treated as comments and ignored.

Generate large credential files from the repo Makefile:

```bash
make gen-credentials COUNT=1000000 OUT=/data/creds-1M.csv
```

---

## 2. Choosing a scenario type

| Type | Simulates | When to use |
|---|---|---|
| `cold_start` | Mass reconnect after outage — Gaussian burst | Primary evaluation scenario. Mirrors real CPE modem behaviour after a BNG reboot. |
| `uniform` | Steady ramp at constant rate | Capacity sizing ("how many auth/sec can this server sustain continuously?") |
| `pessimal` | All subscribers activate simultaneously | Worst-case stress test. Use to find lock contention and queue limits. Not realistic. |
| `coa_storm` | Receive a flood of server-initiated CoA-Requests | Evaluate CoA processing rate when RADIUS server is pushing policy changes to all subscribers. |

**For initial evaluation:** Run `cold_start` first at each scale tier. It produces the most realistic latency distribution and is the scenario you will report to stakeholders.

**For capacity planning:** Run `uniform` to find the server's steady-state auth rate. If `duration_sec = 60` produces `time_to_full_ms` close to 60,000ms, the server is saturated. Halve the subscriber count and re-run.

**For regression testing:** Run `pessimal` at small counts (10k–50k) after each server config change to quickly surface degradation.

---

## 3. Sizing cold_start parameters

The Gaussian activation schedule defines when each subscriber tries to authenticate. The timing is designed to mirror real CPE behaviour: after a BNG reboot, modems start dialing at different times depending on their backoff timers, producing a roughly bell-shaped surge.

### Key parameters

| Parameter | Effect |
|---|---|
| `mu_sec` | The peak of the ramp — most subscribers activate around this time |
| `sigma_sec` | How spread out the ramp is. Larger = slower ramp, smaller = sharper spike |
| `truncate_sigma` | Clamp at `mu ± truncate_sigma × sigma`. 3.0 is standard (covers 99.7% of the normal distribution) |

### Matching your outage recovery profile

The goal is to make `mu_sec` and `sigma_sec` reflect the actual reconnect behaviour of your CPE population.

**Estimate `mu_sec`:** In a real outage, measure the time from BNG link-up to the peak of authentication requests (visible in RADIUS server logs or BNG counters). This is your `mu_sec`.

**Estimate `sigma_sec`:** The width of the reconnect window. If 90% of subscribers reconnect within a 60-second window, that corresponds to roughly ±1.65σ = 30s → σ ≈ 18s. For a very tight reconnect storm (highly synchronized CPE), use `sigma_sec = 5–15`. For a gradual reconnect (diverse CPE with staggered timers), use `sigma_sec = 30–60`.

**Rule of thumb table:**

| Subscriber count | Typical `mu_sec` | Typical `sigma_sec` |
|---|---|---|
| 1k | 10 | 5 |
| 10k | 30 | 10 |
| 100k | 120 | 40 |
| 1M | 300 | 90 |

The total window of meaningful activations is approximately `mu ± 3σ`. For 1M at mu=300, sigma=90: first activation ~30s, last activation ~570s. Set `hard_timeout_sec` well above this (e.g. 900 = 15 minutes) to avoid aborting before the tail.

---

## 4. Sizing source IPs and port range

The RADIUS Identifier is 8 bits. Each `(srcIP, srcPort, dstIP, dstPort)` 4-tuple supports at most 256 concurrent in-flight requests.

### Capacity formula

```
max_concurrent_inflight = num_ips × num_ports × 256
```

### Choosing num_ips and port range

The constraint is peak concurrency — the number of subscribers that have sent an Access-Request but not yet received a reply. This depends on the server's response time and the activation rate.

A practical sizing formula: `peak_concurrency ≈ activation_rate × RTT_99p`

At 100k subscribers with `mu_sec=120`, `sigma_sec=40`:
- Peak activation rate ≈ `100,000 / (sigma_sec × sqrt(2π))` ≈ `100,000 / 100` ≈ 1000 subs/sec
- If RTT p99 = 500ms: peak concurrency ≈ 1000 × 0.5 = 500 in-flight

500 ÷ 256 = ~2 pairs minimum → 2 source IPs is theoretically sufficient, but use 4+ for headroom.

For pessimal at 100k (all in-flight simultaneously):
- 100,000 ÷ 256 ≈ 391 pairs → 4 IPs × 100 ports each = 102,400 pairs ✓

| Scale | Scenario | Recommended source IPs | Port range |
|---|---|---|---|
| 1k | any | 1 | [10000, 20000] |
| 10k | cold_start | 1 | [10000, 20000] |
| 10k | pessimal | 2 | [10000, 20000] |
| 100k | cold_start | 4 | [10000, 20000] |
| 100k | pessimal | 8 | [10000, 20000] |
| 1M | cold_start | 8 | [10000, 20000] |
| 1M | pessimal | 16 | [10000, 60000] |

If a run fails with `ErrIdentifierExhausted` in `run.log`, you are under-provisioned on source IPs. This is a test configuration error, not a RADIUS server failure.

---

## 5. NAS attributes for your BNG model

The RADIUS server must have a NAS entry matching the `nas.ip_address` and `nas.identifier` you configure, with the same shared secret as `target.shared_secret`.

### Standard attributes

```toml
[nas]
ip_address = "10.100.1.1"      # Your radstorm host's IP as seen by the RADIUS server.
                                # Must match a NAS/client entry on the server.
identifier = "bng-01"          # NAS-Identifier. On Huawei BNGs this is typically the hostname.
```

The NAS-Port field (attr 5) is set per-subscriber based on the subscriber's index in the pool. NAS-Port-Id (attr 87) is taken from the `nas_port_id` column of the credentials CSV.

### Framed-IP-Address

Each subscriber gets a mock Framed-IP-Address (attr 8) in its Accounting-Request. The address is generated from the subscriber pool index: `10.x.y.z` where x.y.z encodes the subscriber number. This is realistic enough for most RADIUS server policies that check Framed-IP-Address.

---

## 6. Custom Huawei VSAs

If your RADIUS server applies policies based on Huawei VSAs (vendor-id 2011), add them under `[nas.huawei]`. These are included in every Access-Request sent by radstorm.

```toml
[nas]
ip_address = "10.100.1.1"
identifier = "bng-01"

[nas.huawei]
connect_id         = "1/1/1.100"     # Huawei-Connect-Id (26.2011.20) — slot/port/vlan
service_type       = "internet"      # Huawei-Service-Type (26.2011.65)
# subscriber_qos   = "gold"          # Huawei-Subscriber-QoS-Profile (26.2011.18)
```

The keys in `[nas.huawei]` correspond to Huawei VSA names from the dictionary at `pkg/radius/dictionaries/huawei.dict`. Use the lowercase name without the `Huawei-` prefix.

To verify which Huawei attributes your RADIUS server is receiving, enable debug logging on the server side and inspect a sample Access-Request from a small test run (1k uniform scenario, check `run.log` for packet traces with `--verbose`).

---

## 7. Threshold customization

Thresholds are pass/fail gates evaluated at run end. The default thresholds are `all_established` (100% establishment) and `p99_latency_ms ≤ 5000`. Configure custom thresholds in the `[thresholds]` block to match your SLOs.

**Note:** As of the current release, custom threshold configuration via TOML is not yet exposed — the thresholds block is evaluated from the defaults defined in the collector. To add custom thresholds, configure them in the scenario TOML as follows (this will be active in the first release that exposes threshold config):

```toml
[[thresholds]]
name     = "all_established"
type     = "established_pct"
min      = 100.0                    # 100% of subscribers must establish

[[thresholds]]
name     = "p99_under_2s"
type     = "latency_p99_ms"
max      = 2000                     # p99 must be under 2 seconds

[[thresholds]]
name     = "no_retransmits"
type     = "retransmit_total"
max      = 0                        # Zero tolerance for retransmits (strict SLO)

[[thresholds]]
name     = "p999_under_5s"
type     = "latency_p999_ms"
max      = 5000
```

Until custom threshold config is available, post-process `summary.json` with `jq` to apply your own pass/fail logic:

```bash
# Example: custom p99 threshold of 2000ms
P99=$(jq '.establishment.latency_ms.p99' summary.json)
if [ "$P99" -gt 2000 ]; then
  echo "FAIL: p99=${P99}ms exceeds 2000ms SLO"
  exit 1
fi
```

---

## 8. Validating your config

Always run `validate-config` before a full scenario run. It catches config errors before you spend time waiting for a large run to fail.

```bash
./bin/radstorm validate-config /data/scenarios/coldstart-100k.toml
```

Expected output on a clean config:

```
ok: /data/scenarios/coldstart-100k.toml parses and validates
  scenario.type:     cold_start
  subscribers.count: 100000
  target.auth:       10.0.0.10:1812
  target.acct:       10.0.0.10:1813
  source.ips:        [192.168.100.1 192.168.100.2 192.168.100.3 192.168.100.4]
  output.dir:        /data/results/freeradius/coldstart-100k
  schedule:          100000 activations (last @ 4m52s)
```

Common error messages and fixes:

| Error | Fix |
|---|---|
| `credentials_file: file not found` | Use an absolute path; the path is resolved relative to the working directory of the CLI, not the config file. |
| `subscribers.count exceeds credentials rows` | Credentials CSV has fewer rows than `count`. Generate more credentials or reduce `count`. |
| `source.ips: bind failed on 192.168.100.2` | The alias is not bound on this host. Run `ip addr add 192.168.100.2/24 dev eth0`. |
| `scenario.cold_start: missing (required when type=cold_start)` | You set `type = "cold_start"` but forgot the `[scenario.cold_start]` sub-block. |
| `target.auth_address: resolve failed` | The hostname or IP in `auth_address` is unreachable. Check DNS or use an IP directly. |

Add `--preflight` to also send a single test packet to the RADIUS server:

```bash
./bin/radstorm validate-config --preflight /data/scenarios/coldstart-100k.toml
# preflight: ok
```

If preflight fails, the RADIUS server is unreachable or returning an unexpected response.

---

## 9. Worked examples

### 9.1 100k cold start

Simulates a 100,000-subscriber reconnect storm on a single BNG after a 5-minute outage. Peak reconnect pressure at t=120s.

```toml
[target]
auth_address  = "10.0.0.10:1812"
acct_address  = "10.0.0.10:1813"
shared_secret = "prod-secret"

[coa_listener]
bind_address  = "0.0.0.0:3799"
shared_secret = "prod-secret"

[subscribers]
count               = 100000
credentials_file    = "/data/creds-100k.csv"
auth_method_pap_pct = 80
type_pppoe_pct      = 95
include_acct_start  = true

[source]
ips        = ["192.168.100.1", "192.168.100.2", "192.168.100.3", "192.168.100.4"]
port_range = [10000, 20000]

[nas]
ip_address = "10.100.1.1"
identifier = "bng-01-eval"

[nas.huawei]
connect_id = "1/1/1.100"

[retransmit]
initial_timeout_ms = 5000
max_retries        = 3
backoff            = "exponential"
backoff_base_ms    = 1000

[scenario]
type             = "cold_start"
hard_timeout_sec = 600

[scenario.cold_start]
mu_sec         = 120.0
sigma_sec      = 40.0
truncate_sigma = 3.0

[output]
directory         = "/data/results/freeradius/coldstart-100k"
flush_interval_sec = 30
```

### 9.2 1M cold start

Full-scale run. Requires 8 source IP aliases and 64 GB RAM on the test box.

```toml
[target]
auth_address  = "10.0.0.10:1812"
acct_address  = "10.0.0.10:1813"
shared_secret = "prod-secret"

[coa_listener]
bind_address  = "0.0.0.0:3799"
shared_secret = "prod-secret"

[subscribers]
count               = 1000000
credentials_file    = "/data/creds-1M.csv"
auth_method_pap_pct = 80
type_pppoe_pct      = 95
include_acct_start  = true

[source]
ips = [
  "192.168.100.1", "192.168.100.2", "192.168.100.3", "192.168.100.4",
  "192.168.100.5", "192.168.100.6", "192.168.100.7", "192.168.100.8",
]
port_range = [10000, 20000]

[nas]
ip_address = "10.100.1.1"
identifier = "bng-01-eval"

[retransmit]
initial_timeout_ms = 5000
max_retries        = 3
backoff            = "exponential"
backoff_base_ms    = 1000

[scenario]
type             = "cold_start"
hard_timeout_sec = 1800

[scenario.cold_start]
mu_sec         = 300.0
sigma_sec      = 90.0
truncate_sigma = 3.0

[output]
directory         = "/data/results/freeradius/coldstart-1M"
flush_interval_sec = 30
```

### 9.3 100k CoA storm

Starts the CoA listener and waits for incoming CoA-Requests. The storm itself is fired from the RADIUS server side. See [EVALUATION-GUIDE.md §4.5](EVALUATION-GUIDE.md#45-coa_storm-procedure).

```toml
[target]
auth_address  = "10.0.0.10:1812"
acct_address  = "10.0.0.10:1813"
shared_secret = "prod-secret"

[coa_listener]
bind_address  = "0.0.0.0:3799"
shared_secret = "prod-secret"

[subscribers]
count               = 100000
credentials_file    = "/data/creds-100k.csv"
auth_method_pap_pct = 80
type_pppoe_pct      = 95
include_acct_start  = true

[source]
ips        = ["192.168.100.1", "192.168.100.2", "192.168.100.3", "192.168.100.4"]
port_range = [10000, 20000]

[nas]
ip_address = "10.100.1.1"
identifier = "bng-01-eval"

[scenario]
type             = "coa_storm"
hard_timeout_sec = 600

[output]
directory         = "/data/results/freeradius/coa-100k"
flush_interval_sec = 30
```

### 9.4 Mixed PAP+CHAP at 100k

Realistic mix: 80% PAP (typical for older CPE), 20% CHAP, 5% MAC-auth subscribers.

```toml
[subscribers]
count               = 100000
credentials_file    = "/data/creds-100k-mixed.csv"
auth_method_pap_pct = 80    # 80% PAP, 20% CHAP
type_pppoe_pct      = 95    # 95% PPPoE, 5% MAC-auth
include_acct_start  = true
```

The credentials CSV must include `auth_method` and `sub_type` columns if you want per-subscriber overrides. Otherwise the percentages above apply to the pool as a whole, with assignment determined by subscriber index.
