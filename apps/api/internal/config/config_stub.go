// Package config — local stub of the canonical pkg/config types.
//
// Purpose:
//
//	Minimal struct-only mirror of the Config types defined in
//	.orchestration/contracts/config-schema.md so the API skeleton can compile
//	and unmarshal request bodies before the real pkg/config package (built in
//	parallel by Wave 1B) is merged into main. This file will be DELETED and
//	replaced by `import "github.com/Apextech-sys/reflex-radstorm/pkg/config"`
//	once Wave 1B lands; do not add behaviour here that other packages depend on.
//
// Related files:
//   - .orchestration/contracts/config-schema.md (the canonical contract)
//   - apps/api/internal/server/handlers/runs.go (consumer)
//   - pkg/config/* (the real package, coming from Wave 1B)
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal — temporary shim for the API skeleton only.
package config

// Config is the top-level scenario configuration. Fields mirror the canonical
// schema in config-schema.md exactly (TOML + JSON tags). This is a parsing
// shim only; semantic validation belongs in pkg/config.
type Config struct {
	Target      Target      `toml:"target" json:"target"`
	CoaListener CoaListener `toml:"coa_listener" json:"coa_listener"`
	Subscribers Subscribers `toml:"subscribers" json:"subscribers"`
	Source      Source      `toml:"source" json:"source"`
	NAS         NAS         `toml:"nas" json:"nas"`
	Retransmit  Retransmit  `toml:"retransmit" json:"retransmit"`
	Scenario    Scenario    `toml:"scenario" json:"scenario"`
	Output      Output      `toml:"output" json:"output"`
}

type Target struct {
	AuthAddress  string `toml:"auth_address" json:"auth_address"`
	AcctAddress  string `toml:"acct_address" json:"acct_address"`
	SharedSecret string `toml:"shared_secret" json:"shared_secret"`
}

type CoaListener struct {
	BindAddress  string `toml:"bind_address" json:"bind_address"`
	SharedSecret string `toml:"shared_secret" json:"shared_secret"`
}

type Subscribers struct {
	Count            int    `toml:"count" json:"count"`
	CredentialsFile  string `toml:"credentials_file" json:"credentials_file"`
	AuthMethodPapPct int    `toml:"auth_method_pap_pct" json:"auth_method_pap_pct"`
	TypePppoePct     int    `toml:"type_pppoe_pct" json:"type_pppoe_pct"`
	IncludeAcctStart bool   `toml:"include_acct_start" json:"include_acct_start"`
}

type Source struct {
	IPs       []string `toml:"ips" json:"ips"`
	PortRange [2]int   `toml:"port_range" json:"port_range"`
}

type NAS struct {
	IPAddress  string            `toml:"ip_address" json:"ip_address"`
	Identifier string            `toml:"identifier" json:"identifier"`
	Huawei     map[string]string `toml:"huawei" json:"huawei,omitempty"`
}

type Retransmit struct {
	InitialTimeoutMs int    `toml:"initial_timeout_ms" json:"initial_timeout_ms"`
	MaxRetries       int    `toml:"max_retries" json:"max_retries"`
	Backoff          string `toml:"backoff" json:"backoff"`
	BackoffBaseMs    int    `toml:"backoff_base_ms" json:"backoff_base_ms"`
}

type Scenario struct {
	Type           string        `toml:"type" json:"type"`
	HardTimeoutSec int           `toml:"hard_timeout_sec" json:"hard_timeout_sec"`
	ColdStart      *ColdStartCfg `toml:"cold_start,omitempty" json:"cold_start,omitempty"`
	Uniform        *UniformCfg   `toml:"uniform,omitempty" json:"uniform,omitempty"`
	Pessimal       *PessimalCfg  `toml:"pessimal,omitempty" json:"pessimal,omitempty"`
	CoaStorm       *CoaStormCfg  `toml:"coa_storm,omitempty" json:"coa_storm,omitempty"`
}

type ColdStartCfg struct {
	MuSec         float64 `toml:"mu_sec" json:"mu_sec"`
	SigmaSec      float64 `toml:"sigma_sec" json:"sigma_sec"`
	TruncateSigma float64 `toml:"truncate_sigma" json:"truncate_sigma"`
}

type UniformCfg struct {
	DurationSec float64 `toml:"duration_sec" json:"duration_sec"`
}

type PessimalCfg struct {
	BurstWindowMs int `toml:"burst_window_ms" json:"burst_window_ms"`
}

type CoaStormCfg struct{}

type Output struct {
	Directory        string `toml:"directory" json:"directory"`
	FlushIntervalSec int    `toml:"flush_interval_sec" json:"flush_interval_sec"`
}

// ValidationError describes a single field-level validation failure that the
// API surfaces in 400 responses. Mirrors the shape pkg/config will produce.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validate returns a list of validation errors. The skeleton implementation
// performs only minimal structural checks so the API can return a meaningful
// 400 for obviously empty bodies; full semantic validation arrives with the
// real pkg/config package from Wave 1B.
func Validate(c *Config) []ValidationError {
	var errs []ValidationError
	if c == nil {
		return []ValidationError{{Field: "config", Message: "config is required"}}
	}
	if c.Target.AuthAddress == "" {
		errs = append(errs, ValidationError{Field: "target.auth_address", Message: "is required"})
	}
	if c.Target.AcctAddress == "" {
		errs = append(errs, ValidationError{Field: "target.acct_address", Message: "is required"})
	}
	if c.Target.SharedSecret == "" {
		errs = append(errs, ValidationError{Field: "target.shared_secret", Message: "is required"})
	}
	if c.Subscribers.Count <= 0 {
		errs = append(errs, ValidationError{Field: "subscribers.count", Message: "must be > 0"})
	}
	if c.Scenario.Type == "" {
		errs = append(errs, ValidationError{Field: "scenario.type", Message: "is required"})
	}
	return errs
}
