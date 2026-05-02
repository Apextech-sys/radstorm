// Package config — typed configuration structs for radstorm scenarios.
//
// Purpose:
//   Defines the canonical Go struct representation of a radstorm scenario
//   config, mirroring the frozen contract at
//   .orchestration/contracts/config-schema.md. All fields use struct tags
//   for TOML decoding (BurntSushi/toml), JSON unmarshalling (used by the
//   API server's POST /runs body), and validation (go-playground/validator/v10).
//
// Related files:
//   - pkg/config/load.go (decodes TOML into these structs and applies defaults)
//   - pkg/config/validate.go (runs validator over these structs)
//   - pkg/config/credentials.go (loads the credentials CSV referenced here)
//   - apps/api/internal/server/handlers/runs.go (JSON unmarshalling consumer)
//   - .orchestration/contracts/config-schema.md (frozen contract)
//   - docs/CONFIG.md (human-readable reference)
//
// Briefing: .orchestration/briefings/1b-config.md
//
// Contract: Public Go API consumed by pkg/scenario, apps/cli, apps/api.
// Field-for-field, tag-for-tag mirror of contracts/config-schema.md.
// Both TOML and JSON keys are snake_case to match the rest-api.md examples
// and the frontend zod schema (apps/web/src/lib/types/config.ts).
package config

// Config is the root scenario configuration loaded from TOML or JSON.
type Config struct {
	Target      Target      `toml:"target" json:"target" validate:"required"`
	CoaListener CoaListener `toml:"coa_listener" json:"coa_listener"`
	Subscribers Subscribers `toml:"subscribers" json:"subscribers" validate:"required"`
	Source      Source      `toml:"source" json:"source" validate:"required"`
	NAS         NAS         `toml:"nas" json:"nas" validate:"required"`
	Retransmit  Retransmit  `toml:"retransmit" json:"retransmit"`
	Scenario    Scenario    `toml:"scenario" json:"scenario" validate:"required"`
	Output      Output      `toml:"output" json:"output"`
}

// Target identifies the RADIUS server under test.
type Target struct {
	AuthAddress  string `toml:"auth_address" json:"auth_address" validate:"required,hostport"`
	AcctAddress  string `toml:"acct_address" json:"acct_address" validate:"required,hostport"`
	SharedSecret string `toml:"shared_secret" json:"shared_secret" validate:"required,min=1"`
}

// CoaListener configures the local UDP listener that accepts CoA / Disconnect.
type CoaListener struct {
	BindAddress  string `toml:"bind_address" json:"bind_address" validate:"required,hostport"`
	SharedSecret string `toml:"shared_secret" json:"shared_secret" validate:"required,min=1"`
}

// Subscribers describes the synthetic subscriber pool.
type Subscribers struct {
	Count            int    `toml:"count" json:"count" validate:"required,min=1,max=1000000"`
	CredentialsFile  string `toml:"credentials_file" json:"credentials_file" validate:"required,file"`
	AuthMethodPapPct int    `toml:"auth_method_pap_pct" json:"auth_method_pap_pct" validate:"min=0,max=100"`
	TypePppoePct     int    `toml:"type_pppoe_pct" json:"type_pppoe_pct" validate:"min=0,max=100"`
	IncludeAcctStart bool   `toml:"include_acct_start" json:"include_acct_start"`
}

// Source defines the host-side socket pool.
type Source struct {
	IPs       []string `toml:"ips" json:"ips" validate:"required,min=1,dive,ip"`
	PortRange [2]int   `toml:"port_range" json:"port_range" validate:"required"`
}

// NAS describes the simulated NAS attributes embedded in every Access-Request.
type NAS struct {
	IPAddress  string            `toml:"ip_address" json:"ip_address" validate:"required,ip"`
	Identifier string            `toml:"identifier" json:"identifier" validate:"required"`
	Huawei     map[string]string `toml:"huawei" json:"huawei,omitempty"`
}

// Retransmit governs the retry policy for outstanding RADIUS requests.
type Retransmit struct {
	InitialTimeoutMs int    `toml:"initial_timeout_ms" json:"initial_timeout_ms"`
	MaxRetries       int    `toml:"max_retries" json:"max_retries"`
	Backoff          string `toml:"backoff" json:"backoff"`
	BackoffBaseMs    int    `toml:"backoff_base_ms" json:"backoff_base_ms"`
}

// Scenario selects the activation curve and per-curve parameters.
type Scenario struct {
	Type           string        `toml:"type" json:"type" validate:"required,oneof=cold_start uniform pessimal coa_storm"`
	HardTimeoutSec int           `toml:"hard_timeout_sec" json:"hard_timeout_sec"`
	ColdStart      *ColdStartCfg `toml:"cold_start,omitempty" json:"cold_start,omitempty"`
	Uniform        *UniformCfg   `toml:"uniform,omitempty" json:"uniform,omitempty"`
	Pessimal       *PessimalCfg  `toml:"pessimal,omitempty" json:"pessimal,omitempty"`
	CoaStorm       *CoaStormCfg  `toml:"coa_storm,omitempty" json:"coa_storm,omitempty"`
}

// ColdStartCfg parameterises the truncated Gaussian cold-start activation curve.
type ColdStartCfg struct {
	MuSec         float64 `toml:"mu_sec" json:"mu_sec" validate:"required,gt=0"`
	SigmaSec      float64 `toml:"sigma_sec" json:"sigma_sec" validate:"required,gt=0"`
	TruncateSigma float64 `toml:"truncate_sigma" json:"truncate_sigma" validate:"required,gte=1"`
}

// UniformCfg spreads activations evenly across DurationSec seconds.
type UniformCfg struct {
	DurationSec float64 `toml:"duration_sec" json:"duration_sec" validate:"required,gt=0"`
}

// PessimalCfg fires every subscriber within BurstWindowMs milliseconds.
type PessimalCfg struct {
	BurstWindowMs int `toml:"burst_window_ms" json:"burst_window_ms" validate:"min=0"`
}

// CoaStormCfg is currently a placeholder; the burst is operator-triggered.
type CoaStormCfg struct{}

// Output controls where results are written and how often the collector flushes.
type Output struct {
	Directory        string `toml:"directory" json:"directory" validate:"required"`
	FlushIntervalSec int    `toml:"flush_interval_sec" json:"flush_interval_sec"`
}
