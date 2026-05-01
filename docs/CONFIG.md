# Configuration reference

The authoritative schema lives at [`.orchestration/contracts/config-schema.md`](../.orchestration/contracts/config-schema.md). This file is the human-readable explanation.

## Format

TOML. Loaded by `pkg/config`.

## Example

```toml
# Test target
[target]
auth_address  = "127.0.0.1:1812"
acct_address  = "127.0.0.1:1813"
shared_secret = "testing123"

# CoA / Disconnect listener (we accept server-initiated requests)
[coa_listener]
bind_address  = "0.0.0.0:3799"
shared_secret = "testing123"

# Subscriber pool
[subscribers]
count                = 1000
credentials_file     = "test/fixtures/credentials/1k.csv"
auth_method_pap_pct  = 80      # 80% PAP / 20% CHAP
type_pppoe_pct       = 90      # 90% PPPoE / 10% MAC auth
include_acct_start   = true    # default; set false for auth-only tests

# Source IP pool — must be already bound on this host
[source]
ips        = ["127.0.0.1"]      # at scale: many aliases
port_range = [10000, 60000]

# NAS attributes embedded in every Access-Request
[nas]
ip_address = "10.0.0.1"
identifier = "radstorm-test-nas"
# extra Huawei VSAs you want to set per-test:
# huawei = { connect_id = "1/1/1.100", ... }

# Retransmit policy
[retransmit]
initial_timeout_ms = 5000
max_retries        = 3
backoff            = "exponential"   # exponential | linear | constant
backoff_base_ms    = 1000

# Scenario selection + parameters
[scenario]
type = "cold_start"   # cold_start | uniform | pessimal | coa_storm
hard_timeout_sec = 300

[scenario.cold_start]
mu_sec    = 20.0
sigma_sec = 8.0
truncate_sigma = 3.0   # ramp window = mu ± truncate*sigma

# Output
[output]
directory = "results/"
flush_interval_sec = 30
```

## Field reference

See `.orchestration/contracts/config-schema.md` for the canonical reference.

## Validation

`radstorm validate-config <path>` does:
- TOML parse
- Schema validation (every required field present, all values in range)
- Credentials file existence + count match
- Source IP binding check (sockets actually bind)
- Pre-flight reachability test (single Access-Request to `target.auth_address`, expects reply within 1s)

Returns non-zero exit code if any check fails.
