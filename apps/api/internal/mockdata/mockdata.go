// Package mockdata — hardcoded fixtures used by the Wave 1 API skeleton.
//
// Purpose:
//
//	Centralises every piece of "fake" data the skeleton returns: scenario
//	templates, the canned summary, the establishment curve, the latency
//	histogram, and the SSE progress sequence. Wave 3 replaces these with
//	values derived from the real CLI subprocess output, but the on-the-wire
//	JSON shapes here are the contract the frontend renders against.
//
// Related files:
//   - apps/api/internal/server/handlers/scenarios.go
//   - apps/api/internal/server/handlers/results.go
//   - apps/api/internal/server/handlers/events.go
//   - .orchestration/contracts/results-schema.md
//   - .orchestration/contracts/config-schema.md
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal — values are mocked, but JSON shapes are part of the
// frozen REST/results contracts.
package mockdata

import "github.com/Apextech-sys/radstorm/pkg/config"

// Template describes a single built-in scenario template returned by
// GET /api/v1/scenarios/templates.
type Template struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      *config.Config `json:"config"`
}

// Templates returns the built-in scenario templates. Three templates are
// required by the briefing (smoke-100, cold-start-1k, uniform-1k); a fourth
// pessimal template is included to exercise the pessimal scenario UI.
func Templates() []Template {
	return []Template{
		{
			ID:          "smoke-100",
			Name:        "Smoke 100",
			Description: "100 subscribers, uniform 5s ramp. Sanity check.",
			Config:      smoke100(),
		},
		{
			ID:          "cold-start-1k",
			Name:        "Cold start 1k",
			Description: "1,000 subscribers, Gaussian cold-start ramp (mu=15s, sigma=5s).",
			Config:      coldStart1k(),
		},
		{
			ID:          "uniform-1k",
			Name:        "Uniform 1k",
			Description: "1,000 subscribers spread uniformly over 30 seconds.",
			Config:      uniform1k(),
		},
		{
			ID:          "pessimal-1k",
			Name:        "Pessimal 1k",
			Description: "1,000 subscribers fired in a 100ms burst window.",
			Config:      pessimal1k(),
		},
	}
}

func baseTarget() config.Target {
	return config.Target{
		AuthAddress:  "127.0.0.1:11812",
		AcctAddress:  "127.0.0.1:11813",
		SharedSecret: "testing123",
	}
}

func baseCoa() config.CoaListener {
	return config.CoaListener{
		BindAddress:  "0.0.0.0:13799",
		SharedSecret: "testing123",
	}
}

func baseSource() config.Source {
	return config.Source{
		IPs:       []string{"127.0.0.1"},
		PortRange: [2]int{10000, 60000},
	}
}

func baseNAS() config.NAS {
	return config.NAS{
		IPAddress:  "127.0.0.1",
		Identifier: "radstorm-test",
	}
}

func baseRetransmit() config.Retransmit {
	return config.Retransmit{
		InitialTimeoutMs: 5000,
		MaxRetries:       3,
		Backoff:          "exponential",
		BackoffBaseMs:    1000,
	}
}

func baseOutput() config.Output {
	return config.Output{
		Directory:        "results",
		FlushIntervalSec: 30,
	}
}

func smoke100() *config.Config {
	return &config.Config{
		Target:      baseTarget(),
		CoaListener: baseCoa(),
		Subscribers: config.Subscribers{
			Count:            100,
			CredentialsFile:  "test/fixtures/credentials/smoke-100.csv",
			AuthMethodPapPct: 100,
			TypePppoePct:     100,
			IncludeAcctStart: true,
		},
		Source:     baseSource(),
		NAS:        baseNAS(),
		Retransmit: baseRetransmit(),
		Scenario: config.Scenario{
			Type:           "uniform",
			HardTimeoutSec: 60,
			Uniform:        &config.UniformCfg{DurationSec: 5},
		},
		Output: baseOutput(),
	}
}

func coldStart1k() *config.Config {
	return &config.Config{
		Target:      baseTarget(),
		CoaListener: baseCoa(),
		Subscribers: config.Subscribers{
			Count:            1000,
			CredentialsFile:  "test/fixtures/credentials/test-1k.csv",
			AuthMethodPapPct: 100,
			TypePppoePct:     100,
			IncludeAcctStart: true,
		},
		Source:     baseSource(),
		NAS:        baseNAS(),
		Retransmit: baseRetransmit(),
		Scenario: config.Scenario{
			Type:           "cold_start",
			HardTimeoutSec: 120,
			ColdStart: &config.ColdStartCfg{
				MuSec:         15,
				SigmaSec:      5,
				TruncateSigma: 3,
			},
		},
		Output: baseOutput(),
	}
}

func uniform1k() *config.Config {
	return &config.Config{
		Target:      baseTarget(),
		CoaListener: baseCoa(),
		Subscribers: config.Subscribers{
			Count:            1000,
			CredentialsFile:  "test/fixtures/credentials/test-1k.csv",
			AuthMethodPapPct: 100,
			TypePppoePct:     100,
			IncludeAcctStart: true,
		},
		Source:     baseSource(),
		NAS:        baseNAS(),
		Retransmit: baseRetransmit(),
		Scenario: config.Scenario{
			Type:           "uniform",
			HardTimeoutSec: 120,
			Uniform:        &config.UniformCfg{DurationSec: 30},
		},
		Output: baseOutput(),
	}
}

func pessimal1k() *config.Config {
	return &config.Config{
		Target:      baseTarget(),
		CoaListener: baseCoa(),
		Subscribers: config.Subscribers{
			Count:            1000,
			CredentialsFile:  "test/fixtures/credentials/test-1k.csv",
			AuthMethodPapPct: 100,
			TypePppoePct:     100,
			IncludeAcctStart: true,
		},
		Source:     baseSource(),
		NAS:        baseNAS(),
		Retransmit: baseRetransmit(),
		Scenario: config.Scenario{
			Type:           "pessimal",
			HardTimeoutSec: 60,
			Pessimal:       &config.PessimalCfg{BurstWindowMs: 100},
		},
		Output: baseOutput(),
	}
}

// Summary returns the canned summary.json blob used by both the SSE
// `complete` event and GET /api/v1/runs/{id}/results. Shape per
// .orchestration/contracts/results-schema.md.
func Summary(runID string) map[string]any {
	return map[string]any{
		"run_id":        runID,
		"config_hash":   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"started_at":    "2026-05-02T01:23:45.123Z",
		"finished_at":   "2026-05-02T01:24:30.456Z",
		"duration_ms":   45333,
		"scenario_type": "cold_start",
		"outcome":       "succeeded",
		"subscribers": map[string]any{
			"total":                  1000,
			"established":            998,
			"auth_failed":            1,
			"acct_failed":            1,
			"terminated":             0,
			"still_in_flight_at_end": 0,
		},
		"establishment": map[string]any{
			"time_to_first_ms": 87,
			"time_to_full_ms":  32450,
			"latency_ms": map[string]any{
				"min":  12,
				"p50":  145,
				"p95":  890,
				"p99":  2300,
				"p999": 4100,
				"max":  4500,
			},
			"curve": EstablishmentCurve()["points"],
		},
		"retransmits": map[string]any{
			"total":                       27,
			"subscribers_with_retransmit": 24,
			"per_subscriber_distribution": map[string]any{
				"p50": 0,
				"p95": 0,
				"p99": 1,
				"max": 3,
			},
		},
		"coa": map[string]any{
			"received":            0,
			"acked":               0,
			"naked":               0,
			"dropped":             0,
			"response_latency_us": nil,
		},
		"disconnect": map[string]any{
			"received":            0,
			"acked":               0,
			"naked":               0,
			"response_latency_us": nil,
		},
		"server_health": map[string]any{
			"unresponsive_periods": []any{},
			"error_responses":      0,
		},
		"thresholds": map[string]any{
			"evaluated": []any{
				map[string]any{"name": "all_established", "expected": "established_count == total", "actual": "998 == 1000", "pass": false},
				map[string]any{"name": "p99_latency_ms", "expected": "<= 5000", "actual": 2300, "pass": true},
			},
			"overall": "fail",
		},
		"artifacts": map[string]any{
			"events_parquet":      "events.parquet",
			"subscribers_parquet": "subscribers.parquet",
			"run_log":             "run.log",
		},
	}
}

// EstablishmentCurve returns the canned time-series for the establishment
// chart. Shape per rest-api.md GET /runs/{id}/results/establishment-curve.
func EstablishmentCurve() map[string]any {
	return map[string]any{
		"interval_ms": 1000,
		"points": []map[string]any{
			{"offset_ms": 0, "established": 0, "activated": 0, "in_flight": 0},
			{"offset_ms": 1000, "established": 12, "activated": 18, "in_flight": 6},
			{"offset_ms": 5000, "established": 187, "activated": 240, "in_flight": 53},
			{"offset_ms": 10000, "established": 540, "activated": 612, "in_flight": 72},
			{"offset_ms": 15000, "established": 821, "activated": 870, "in_flight": 49},
			{"offset_ms": 20000, "established": 940, "activated": 962, "in_flight": 22},
			{"offset_ms": 25000, "established": 985, "activated": 996, "in_flight": 11},
			{"offset_ms": 30000, "established": 996, "activated": 1000, "in_flight": 4},
			{"offset_ms": 35000, "established": 998, "activated": 1000, "in_flight": 0},
		},
	}
}

// LatencyHistogram returns the canned p* bucket counts.
func LatencyHistogram() map[string]any {
	return map[string]any{
		"buckets": []map[string]any{
			{"le_ms": 100, "count": 850},
			{"le_ms": 500, "count": 980},
			{"le_ms": 1000, "count": 998},
			{"le_ms": 5000, "count": 1000},
		},
	}
}

// ProgressEvent is one step in the canned SSE progress sequence.
type ProgressEvent struct {
	OffsetMs    int `json:"offset_ms"`
	Activated   int `json:"activated"`
	Established int `json:"established"`
	Failed      int `json:"failed"`
	InFlight    int `json:"in_flight"`
	Retransmits int `json:"retransmits"`
}

// ProgressSequence returns the 8 fake progress snapshots emitted by the SSE
// stream. Roughly mirrors the establishment curve so the frontend's progress
// chart and final summary line up visually.
func ProgressSequence() []ProgressEvent {
	return []ProgressEvent{
		{OffsetMs: 500, Activated: 18, Established: 12, Failed: 0, InFlight: 6, Retransmits: 0},
		{OffsetMs: 1500, Activated: 240, Established: 187, Failed: 0, InFlight: 53, Retransmits: 2},
		{OffsetMs: 2500, Activated: 612, Established: 540, Failed: 0, InFlight: 72, Retransmits: 8},
		{OffsetMs: 3500, Activated: 870, Established: 821, Failed: 0, InFlight: 49, Retransmits: 15},
		{OffsetMs: 4500, Activated: 962, Established: 940, Failed: 0, InFlight: 22, Retransmits: 22},
		{OffsetMs: 5500, Activated: 996, Established: 985, Failed: 1, InFlight: 11, Retransmits: 25},
		{OffsetMs: 6500, Activated: 1000, Established: 996, Failed: 1, InFlight: 4, Retransmits: 27},
		{OffsetMs: 7500, Activated: 1000, Established: 998, Failed: 2, InFlight: 0, Retransmits: 27},
	}
}
