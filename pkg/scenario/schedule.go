// Package scenario — activation schedule generation.
//
// Purpose:
//
//	Produces the per-subscriber activation offsets (relative to T0) for
//	each scenario type defined in pkg/config.Scenario.Type. The scenario
//	driver (runner.go) consumes the generated []time.Duration to pace
//	when each Subscriber.Run goroutine starts.
//
//	Three families:
//	  - cold_start: truncated Gaussian centred on Mu, clamped to ±k·sigma
//	  - uniform:    evenly-spread across DurationSec
//	  - pessimal:   all subscribers within a tight BurstWindowMs
//	  - coa_storm:  pre-established subscribers; no activation curve
//	    (returns an empty slice — the runner just listens)
//
// Related files:
//   - pkg/scenario/runner.go      (consumes the generated schedule)
//   - pkg/scenario/lifecycle.go   (Phase the schedule runs inside)
//   - pkg/config/config.go        (ColdStartCfg / UniformCfg / PessimalCfg)
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
//
// Contract: Public — Schedule and BuildSchedule are consumed by runner.go
// and exercised directly by the CLI's `validate-config --explain` mode.
package scenario

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/config"
)

// Schedule is the per-subscriber activation timeline.
//
// Offsets[i] is how long after T0 the i-th subscriber should activate.
// Always sorted ascending so the runner can iterate without re-sorting.
type Schedule struct {
	// Offsets is sorted ascending. Length == cfg.Subscribers.Count
	// (except for coa_storm where it is 0 — the scenario relies on
	// pre-established subscribers and the listener).
	Offsets []time.Duration

	// Type echoes the scenario type the schedule was built for. Used by
	// the runner for lifecycle decisions and by summary reporting.
	Type string
}

// Total returns the number of activations.
func (s *Schedule) Total() int { return len(s.Offsets) }

// Last returns the last activation offset, or 0 when empty.
func (s *Schedule) Last() time.Duration {
	if len(s.Offsets) == 0 {
		return 0
	}
	return s.Offsets[len(s.Offsets)-1]
}

// BuildSchedule generates the activation offsets for the configured
// scenario type. seed lets tests pin determinism; pass 0 for the default
// time-based seed used in production.
//
// Errors:
//   - unknown scenario.type
//   - missing per-type sub-config (e.g. type="cold_start" with cfg.ColdStart == nil)
//   - non-positive parameters (negative sigma, zero duration)
func BuildSchedule(cfg *config.Config, seed int64) (*Schedule, error) {
	if cfg == nil {
		return nil, errors.New("scenario: config is nil")
	}
	n := cfg.Subscribers.Count
	if n < 0 {
		return nil, fmt.Errorf("scenario: subscribers.count is negative (%d)", n)
	}

	switch cfg.Scenario.Type {
	case "cold_start":
		if cfg.Scenario.ColdStart == nil {
			return nil, errors.New("scenario: type=cold_start requires [scenario.cold_start] block")
		}
		return buildColdStart(n, cfg.Scenario.ColdStart, seed)
	case "uniform":
		if cfg.Scenario.Uniform == nil {
			return nil, errors.New("scenario: type=uniform requires [scenario.uniform] block")
		}
		return buildUniform(n, cfg.Scenario.Uniform)
	case "pessimal":
		// Pessimal sub-config is optional; missing block ⇒ zero burst window.
		burstMs := 0
		if cfg.Scenario.Pessimal != nil {
			burstMs = cfg.Scenario.Pessimal.BurstWindowMs
		}
		return buildPessimal(n, burstMs)
	case "coa_storm":
		// CoA storm has no activation schedule — subscribers are
		// pre-established (or seeded by an earlier phase) and the
		// scenario just runs the listener and waits HardTimeoutSec.
		return &Schedule{Offsets: nil, Type: "coa_storm"}, nil
	default:
		return nil, fmt.Errorf("scenario: unknown scenario.type %q", cfg.Scenario.Type)
	}
}

// buildColdStart generates a truncated-Gaussian activation curve. Each
// subscriber's offset is sampled from N(mu, sigma) and clamped to
// [mu - k*sigma, mu + k*sigma] (k = TruncateSigma). Offsets below zero
// are also clamped to zero so the runner never schedules into the past.
func buildColdStart(n int, ccs *config.ColdStartCfg, seed int64) (*Schedule, error) {
	if ccs.SigmaSec <= 0 {
		return nil, fmt.Errorf("scenario: cold_start.sigma_sec must be > 0, got %g", ccs.SigmaSec)
	}
	if ccs.MuSec < 0 {
		return nil, fmt.Errorf("scenario: cold_start.mu_sec must be >= 0, got %g", ccs.MuSec)
	}
	k := ccs.TruncateSigma
	if k < 1 {
		k = 3.0
	}

	r := rand.New(rand.NewSource(seedOrTime(seed)))

	mu := ccs.MuSec
	sigma := ccs.SigmaSec
	loSec := mu - k*sigma
	hiSec := mu + k*sigma
	if loSec < 0 {
		loSec = 0
	}

	out := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		secs := r.NormFloat64()*sigma + mu
		if secs < loSec {
			secs = loSec
		}
		if secs > hiSec {
			secs = hiSec
		}
		out = append(out, secondsToDuration(secs))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return &Schedule{Offsets: out, Type: "cold_start"}, nil
}

// buildUniform spreads N activations evenly across DurationSec seconds.
// offset_i = i * DurationSec / N. The 0th subscriber starts at T0 and
// the (N-1)-th starts at DurationSec * (N-1)/N. With a single subscriber
// it activates at T0.
func buildUniform(n int, ucs *config.UniformCfg) (*Schedule, error) {
	if ucs.DurationSec <= 0 {
		return nil, fmt.Errorf("scenario: uniform.duration_sec must be > 0, got %g", ucs.DurationSec)
	}
	out := make([]time.Duration, 0, n)
	if n == 0 {
		return &Schedule{Offsets: out, Type: "uniform"}, nil
	}
	step := ucs.DurationSec / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, secondsToDuration(float64(i)*step))
	}
	return &Schedule{Offsets: out, Type: "uniform"}, nil
}

// buildPessimal places every subscriber within BurstWindowMs of T0. With
// BurstWindowMs == 0 (or missing) all activations are simultaneous at T0.
// offset_i = (i / N) * BurstWindowMs, so i=0 is at 0 and i=N-1 is just
// inside the window.
func buildPessimal(n, burstWindowMs int) (*Schedule, error) {
	if burstWindowMs < 0 {
		return nil, fmt.Errorf("scenario: pessimal.burst_window_ms must be >= 0, got %d", burstWindowMs)
	}
	out := make([]time.Duration, 0, n)
	if n == 0 || burstWindowMs == 0 {
		for i := 0; i < n; i++ {
			out = append(out, 0)
		}
		return &Schedule{Offsets: out, Type: "pessimal"}, nil
	}
	step := float64(burstWindowMs) / float64(n)
	for i := 0; i < n; i++ {
		ms := float64(i) * step
		out = append(out, time.Duration(ms*float64(time.Millisecond)))
	}
	return &Schedule{Offsets: out, Type: "pessimal"}, nil
}

// secondsToDuration converts a fractional-seconds value to time.Duration
// without losing sub-second precision (vs. time.Duration(int(secs))*Sec).
func secondsToDuration(secs float64) time.Duration {
	if math.IsNaN(secs) || math.IsInf(secs, 0) {
		return 0
	}
	if secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// seedOrTime returns seed when non-zero, else a time-based seed. Centralised
// so the cold_start path picks the correct source consistently.
func seedOrTime(seed int64) int64 {
	if seed != 0 {
		return seed
	}
	return time.Now().UnixNano()
}
