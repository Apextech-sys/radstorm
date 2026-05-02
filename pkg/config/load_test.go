// Package config — tests for Load, LoadConfigOnly, and ApplyDefaults.
//
// Purpose:
//
//	Exercises the disk-side workflow: TOML decode, defaults application,
//	and the wiring through to the credentials loader. Uses the fixtures
//	under pkg/config/testdata/.
//
// Related files:
//   - pkg/config/load.go (the system under test)
//   - pkg/config/testdata/*.toml (fixtures)
//
// Briefing: .orchestration/briefings/1b-config.md
//
// Contract: internal — test code only.
package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Example_FullyPopulated(t *testing.T) {
	cfg, creds, err := Load("testdata/example.toml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "127.0.0.1:1812", cfg.Target.AuthAddress)
	assert.Equal(t, "127.0.0.1:1813", cfg.Target.AcctAddress)
	assert.Equal(t, "testing123", cfg.Target.SharedSecret)

	assert.Equal(t, "0.0.0.0:3799", cfg.CoaListener.BindAddress)
	assert.Equal(t, "testing123", cfg.CoaListener.SharedSecret)

	assert.Equal(t, 3, cfg.Subscribers.Count)
	assert.Equal(t, 80, cfg.Subscribers.AuthMethodPapPct)
	assert.Equal(t, 90, cfg.Subscribers.TypePppoePct)
	assert.True(t, cfg.Subscribers.IncludeAcctStart)

	assert.Equal(t, []string{"127.0.0.1"}, cfg.Source.IPs)
	assert.Equal(t, [2]int{10000, 60000}, cfg.Source.PortRange)

	assert.Equal(t, "10.0.0.1", cfg.NAS.IPAddress)
	assert.Equal(t, "radstorm-test-nas", cfg.NAS.Identifier)

	assert.Equal(t, 5000, cfg.Retransmit.InitialTimeoutMs)
	assert.Equal(t, 3, cfg.Retransmit.MaxRetries)
	assert.Equal(t, "exponential", cfg.Retransmit.Backoff)
	assert.Equal(t, 1000, cfg.Retransmit.BackoffBaseMs)

	assert.Equal(t, "cold_start", cfg.Scenario.Type)
	assert.Equal(t, 300, cfg.Scenario.HardTimeoutSec)
	require.NotNil(t, cfg.Scenario.ColdStart)
	assert.InDelta(t, 20.0, cfg.Scenario.ColdStart.MuSec, 0.0001)
	assert.InDelta(t, 8.0, cfg.Scenario.ColdStart.SigmaSec, 0.0001)
	assert.InDelta(t, 3.0, cfg.Scenario.ColdStart.TruncateSigma, 0.0001)

	assert.Equal(t, "results/", cfg.Output.Directory)
	assert.Equal(t, 30, cfg.Output.FlushIntervalSec)

	// minimal.csv has 3 rows; subscribers.count == 3.
	assert.Len(t, creds, 3)
}

func TestLoadConfigOnly_AppliesDefaults(t *testing.T) {
	cfg, err := LoadConfigOnly("testdata/minimal_defaults.toml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// All defaultable fields should hold the values from the contract's
	// defaults table.
	assert.Equal(t, 100, cfg.Subscribers.AuthMethodPapPct, "auth_method_pap_pct default")
	assert.Equal(t, 100, cfg.Subscribers.TypePppoePct, "type_pppoe_pct default")
	assert.True(t, cfg.Subscribers.IncludeAcctStart, "include_acct_start default")

	assert.Equal(t, 5000, cfg.Retransmit.InitialTimeoutMs)
	assert.Equal(t, 3, cfg.Retransmit.MaxRetries)
	assert.Equal(t, "exponential", cfg.Retransmit.Backoff)
	assert.Equal(t, 1000, cfg.Retransmit.BackoffBaseMs)

	assert.Equal(t, 300, cfg.Scenario.HardTimeoutSec)

	assert.Equal(t, "results", cfg.Output.Directory)
	assert.Equal(t, 30, cfg.Output.FlushIntervalSec)

	assert.Equal(t, [2]int{10000, 60000}, cfg.Source.PortRange)
}

func TestLoad_MissingSharedSecret(t *testing.T) {
	_, _, err := Load("testdata/missing_secret.toml")
	require.Error(t, err)
	// Validator's error mentions the field path; check both common forms.
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "SharedSecret") || strings.Contains(msg, "shared_secret"),
		"error should mention shared secret field: %s", msg,
	)
}

func TestLoad_BadPapPct(t *testing.T) {
	_, _, err := Load("testdata/bad_pap_pct.toml")
	require.Error(t, err)
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "AuthMethodPapPct") || strings.Contains(msg, "auth_method_pap_pct"),
		"error should mention pap pct field: %s", msg,
	)
}

func TestLoad_BadScenarioType(t *testing.T) {
	_, _, err := Load("testdata/bad_scenario_type.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oneof")
}

func TestLoad_BadHostport(t *testing.T) {
	_, _, err := Load("testdata/bad_hostport.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostport")
}

func TestLoad_MissingCredentialsFile(t *testing.T) {
	_, _, err := Load("testdata/bad_credentials_path.toml")
	require.Error(t, err)
	// The file validator triggers during Validate, not the credentials loader.
	assert.Contains(t, err.Error(), "file")
}

func TestLoad_BadToml(t *testing.T) {
	_, _, err := Load("testdata/bad_toml.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing TOML")
}

func TestLoad_NonexistentPath(t *testing.T) {
	_, _, err := Load("testdata/does_not_exist.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config")
}

func TestLoadConfigOnly_EmptyPath(t *testing.T) {
	_, err := LoadConfigOnly("")
	require.Error(t, err)
}

func TestApplyDefaults_NilSafe(t *testing.T) {
	// Should not panic.
	ApplyDefaults(nil)
}

func TestApplyDefaults_ZeroValuesPopulated(t *testing.T) {
	// When called without metadata, ApplyDefaults treats zero/empty as
	// missing for the documented defaultable fields.
	cfg := &Config{}
	ApplyDefaults(cfg)

	assert.Equal(t, 100, cfg.Subscribers.AuthMethodPapPct)
	assert.Equal(t, 100, cfg.Subscribers.TypePppoePct)
	assert.True(t, cfg.Subscribers.IncludeAcctStart)
	assert.Equal(t, 5000, cfg.Retransmit.InitialTimeoutMs)
	assert.Equal(t, 3, cfg.Retransmit.MaxRetries)
	assert.Equal(t, "exponential", cfg.Retransmit.Backoff)
	assert.Equal(t, 1000, cfg.Retransmit.BackoffBaseMs)
	assert.Equal(t, 300, cfg.Scenario.HardTimeoutSec)
	assert.Equal(t, "results", cfg.Output.Directory)
	assert.Equal(t, 30, cfg.Output.FlushIntervalSec)
	assert.Equal(t, [2]int{10000, 60000}, cfg.Source.PortRange)
}

func TestSplitTomlKey(t *testing.T) {
	assert.Equal(t, []string{"a"}, splitTomlKey("a"))
	assert.Equal(t, []string{"a", "b"}, splitTomlKey("a.b"))
	assert.Equal(t, []string{"a", "b", "c"}, splitTomlKey("a.b.c"))
}
