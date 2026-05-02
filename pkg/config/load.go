// Package config — TOML loading, defaults, and the public Load entry points.
//
// Purpose:
//
//	Owns the disk-side workflow: read a TOML file, decode it into Config,
//	apply documented defaults, validate, and (for the convenience entry
//	point) load the referenced credentials CSV.
//
// Related files:
//   - pkg/config/config.go (struct definitions decoded into)
//   - pkg/config/validate.go (validate.Struct invoked here)
//   - pkg/config/credentials.go (LoadCredentials called from Load)
//   - .orchestration/contracts/config-schema.md (defaults table mirrored)
//
// Briefing: .orchestration/briefings/1b-config.md
//
// Contract:
//
//	Public functions Load, LoadConfigOnly, ApplyDefaults form the entire
//	stable surface of pkg/config used by the rest of the system.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Load reads, validates, and returns the typed Config plus the credentials
// referenced by Subscribers.CredentialsFile. It is the single entry point for
// the production CLI/API path.
func Load(path string) (*Config, []Credential, error) {
	cfg, err := LoadConfigOnly(path)
	if err != nil {
		return nil, nil, err
	}
	creds, err := LoadCredentials(cfg.Subscribers.CredentialsFile)
	if err != nil {
		return nil, nil, fmt.Errorf("loading credentials: %w", err)
	}
	if len(creds) < cfg.Subscribers.Count {
		return nil, nil, fmt.Errorf(
			"credentials file %q has %d rows; need at least %d for subscribers.count",
			cfg.Subscribers.CredentialsFile, len(creds), cfg.Subscribers.Count,
		)
	}
	return cfg, creds, nil
}

// LoadConfigOnly performs TOML decode + defaults + validation but does not
// touch the credentials file. Used by the `validate-config` CLI subcommand
// and by tests that want to inspect the parsed config without side effects.
func LoadConfigOnly(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	cfg := &Config{}
	// MetaData lets us detect which keys were actually present so defaults can
	// distinguish "user wrote 0" from "user omitted the field".
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing TOML %q: %w", path, err)
	}

	ApplyDefaultsWithMeta(cfg, &md)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config %q: %w", path, err)
	}
	return cfg, nil
}

// ApplyDefaults applies the defaults table from
// .orchestration/contracts/config-schema.md. It is exported so tests can
// drive it directly.
//
// Without TOML metadata we cannot tell whether a zero numeric value came from
// the file or from Go's zero value, so this overload conservatively treats
// any zero/empty field as "missing" for the documented defaultable fields.
// Callers that have the toml.MetaData should prefer ApplyDefaultsWithMeta.
func ApplyDefaults(cfg *Config) {
	ApplyDefaultsWithMeta(cfg, nil)
}

// ApplyDefaultsWithMeta is the metadata-aware variant: a default is applied
// only if the corresponding TOML key was NOT present in the source file.
// When meta is nil it falls back to the conservative "zero means missing"
// strategy, which is correct for defaults whose documented value is non-zero.
func ApplyDefaultsWithMeta(cfg *Config, meta *toml.MetaData) {
	if cfg == nil {
		return
	}

	missing := func(key string) bool {
		if meta == nil {
			return true
		}
		return !meta.IsDefined(splitTomlKey(key)...)
	}

	// Subscribers
	if missing("subscribers.auth_method_pap_pct") && cfg.Subscribers.AuthMethodPapPct == 0 {
		cfg.Subscribers.AuthMethodPapPct = 100
	}
	if missing("subscribers.type_pppoe_pct") && cfg.Subscribers.TypePppoePct == 0 {
		cfg.Subscribers.TypePppoePct = 100
	}
	if missing("subscribers.include_acct_start") {
		cfg.Subscribers.IncludeAcctStart = true
	}

	// Retransmit
	if missing("retransmit.initial_timeout_ms") && cfg.Retransmit.InitialTimeoutMs == 0 {
		cfg.Retransmit.InitialTimeoutMs = 5000
	}
	if missing("retransmit.max_retries") && cfg.Retransmit.MaxRetries == 0 {
		cfg.Retransmit.MaxRetries = 3
	}
	if missing("retransmit.backoff") && cfg.Retransmit.Backoff == "" {
		cfg.Retransmit.Backoff = "exponential"
	}
	if missing("retransmit.backoff_base_ms") && cfg.Retransmit.BackoffBaseMs == 0 {
		cfg.Retransmit.BackoffBaseMs = 1000
	}

	// Scenario
	if missing("scenario.hard_timeout_sec") && cfg.Scenario.HardTimeoutSec == 0 {
		cfg.Scenario.HardTimeoutSec = 300
	}

	// Output
	if missing("output.directory") && cfg.Output.Directory == "" {
		cfg.Output.Directory = "results"
	}
	if missing("output.flush_interval_sec") && cfg.Output.FlushIntervalSec == 0 {
		cfg.Output.FlushIntervalSec = 30
	}

	// Source.PortRange — default [10000, 60000] if both elements are zero.
	if missing("source.port_range") && cfg.Source.PortRange == [2]int{0, 0} {
		cfg.Source.PortRange = [2]int{10000, 60000}
	}
}

// splitTomlKey turns "a.b.c" into []string{"a","b","c"} for MetaData.IsDefined.
func splitTomlKey(key string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			out = append(out, key[start:i])
			start = i + 1
		}
	}
	out = append(out, key[start:])
	return out
}
