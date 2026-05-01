# Contract: Config schema

**Owners:** `pkg/config` (Go struct + TOML loader), `apps/web/lib/config.ts` (mirror types + zod schema), CONFIG.md (human reference).

**Stability:** Frozen after Wave 1. Changes propagate to all three.

---

## Go struct (canonical)

```go
package config

import "time"

type Config struct {
    Target       Target       `toml:"target" validate:"required"`
    CoaListener  CoaListener  `toml:"coa_listener"`
    Subscribers  Subscribers  `toml:"subscribers" validate:"required"`
    Source       Source       `toml:"source" validate:"required"`
    NAS          NAS          `toml:"nas" validate:"required"`
    Retransmit   Retransmit   `toml:"retransmit"`
    Scenario     Scenario     `toml:"scenario" validate:"required"`
    Output       Output       `toml:"output"`
}

type Target struct {
    AuthAddress  string `toml:"auth_address" validate:"required,hostport"`   // "host:port"
    AcctAddress  string `toml:"acct_address" validate:"required,hostport"`
    SharedSecret string `toml:"shared_secret" validate:"required,min=1"`
}

type CoaListener struct {
    BindAddress  string `toml:"bind_address" validate:"required,hostport"`
    SharedSecret string `toml:"shared_secret" validate:"required,min=1"`
}

type Subscribers struct {
    Count             int    `toml:"count" validate:"required,min=1,max=1000000"`
    CredentialsFile   string `toml:"credentials_file" validate:"required,file"`
    AuthMethodPapPct  int    `toml:"auth_method_pap_pct" validate:"min=0,max=100"`
    TypePppoePct      int    `toml:"type_pppoe_pct" validate:"min=0,max=100"`
    IncludeAcctStart  bool   `toml:"include_acct_start"`
}

type Source struct {
    IPs       []string `toml:"ips" validate:"required,min=1,dive,ip"`
    PortRange [2]int   `toml:"port_range" validate:"required"`  // [low, high]
}

type NAS struct {
    IPAddress  string            `toml:"ip_address" validate:"required,ip"`
    Identifier string            `toml:"identifier" validate:"required"`
    Huawei     map[string]string `toml:"huawei"`  // optional VSAs
}

type Retransmit struct {
    InitialTimeoutMs int    `toml:"initial_timeout_ms"`
    MaxRetries       int    `toml:"max_retries"`
    Backoff          string `toml:"backoff"`             // "exponential" | "linear" | "constant"
    BackoffBaseMs    int    `toml:"backoff_base_ms"`
}

type Scenario struct {
    Type           string         `toml:"type" validate:"required,oneof=cold_start uniform pessimal coa_storm"`
    HardTimeoutSec int            `toml:"hard_timeout_sec"`
    ColdStart      *ColdStartCfg  `toml:"cold_start,omitempty"`
    Uniform        *UniformCfg    `toml:"uniform,omitempty"`
    Pessimal       *PessimalCfg   `toml:"pessimal,omitempty"`
    CoaStorm       *CoaStormCfg   `toml:"coa_storm,omitempty"`
}

type ColdStartCfg struct {
    MuSec         float64 `toml:"mu_sec" validate:"required,gt=0"`
    SigmaSec      float64 `toml:"sigma_sec" validate:"required,gt=0"`
    TruncateSigma float64 `toml:"truncate_sigma" validate:"required,gte=1"`
}

type UniformCfg struct {
    DurationSec float64 `toml:"duration_sec" validate:"required,gt=0"`
}

type PessimalCfg struct {
    BurstWindowMs int `toml:"burst_window_ms" validate:"min=0"`
}

type CoaStormCfg struct {
    // For now: just the listener config; the burst itself is operator-triggered
    // upstream (e.g. via FreeRADIUS' radclient). The runner just captures.
}

type Output struct {
    Directory        string `toml:"directory" validate:"required"`
    FlushIntervalSec int    `toml:"flush_interval_sec"`
}
```

## Defaults (applied if field missing)

| Field | Default |
|---|---|
| `Subscribers.AuthMethodPapPct` | `100` (all PAP) |
| `Subscribers.TypePppoePct` | `100` (all PPPoE) |
| `Subscribers.IncludeAcctStart` | `true` |
| `Retransmit.InitialTimeoutMs` | `5000` |
| `Retransmit.MaxRetries` | `3` |
| `Retransmit.Backoff` | `"exponential"` |
| `Retransmit.BackoffBaseMs` | `1000` |
| `Scenario.HardTimeoutSec` | `300` |
| `Output.Directory` | `"results"` |
| `Output.FlushIntervalSec` | `30` |
| `Source.PortRange` | `[10000, 60000]` |

## TypeScript mirror (apps/web/lib/types/config.ts)

The frontend's zod schema MUST mirror this exactly. Validation rules:
- All number ranges match
- All `oneof` enums match
- Optional vs required fields match
- The frontend zod schema is generated or hand-maintained from this contract — change them together

## Credentials file format

CSV with header row. Required columns:
- `username` (string)
- `password` (string)
- `auth_method` (optional: `pap` | `chap`; if absent, follows distribution config)
- `sub_type` (optional: `pppoe` | `mac`; if absent, follows distribution config)
- `nas_port_id` (optional: string)
- `mac_address` (optional: hex string for Calling-Station-Id)

Example:
```csv
username,password,auth_method,sub_type,nas_port_id,mac_address
sub00000001,pw00000001,pap,pppoe,1/1/1.100,aa:bb:cc:00:00:01
sub00000002,pw00000002,chap,pppoe,1/1/1.101,aa:bb:cc:00:00:02
```
