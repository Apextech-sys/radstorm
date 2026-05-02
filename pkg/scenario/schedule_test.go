// Package scenario — tests for activation schedule generation.
//
// Purpose:
//
//	Verifies the four schedule families behave per docs/PROTOCOL.md:
//	  - cold_start: N samples, mean ≈ μ, std ≈ σ, all within ±k·σ
//	  - uniform:    evenly-spaced offsets, ascending
//	  - pessimal:   every offset within BurstWindowMs
//	  - coa_storm:  empty schedule (no activation curve)
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package scenario

import (
	"math"
	"sort"
	"testing"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
)

func TestBuildSchedule_ColdStart_StatsMatchGaussian(t *testing.T) {
	const n = 1000
	const mu = 5.0
	const sigma = 1.0
	const k = 3.0

	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: n},
		Scenario: config.Scenario{
			Type: "cold_start",
			ColdStart: &config.ColdStartCfg{
				MuSec:         mu,
				SigmaSec:      sigma,
				TruncateSigma: k,
			},
		},
	}

	sch, err := BuildSchedule(cfg, 42)
	if err != nil {
		t.Fatalf("BuildSchedule cold_start: %v", err)
	}
	if got := sch.Total(); got != n {
		t.Fatalf("Total = %d, want %d", got, n)
	}

	// Sorted ascending invariant.
	for i := 1; i < len(sch.Offsets); i++ {
		if sch.Offsets[i] < sch.Offsets[i-1] {
			t.Fatalf("offsets not sorted at i=%d: %v < %v", i, sch.Offsets[i], sch.Offsets[i-1])
		}
	}

	loSec := mu - k*sigma
	if loSec < 0 {
		loSec = 0
	}
	hiSec := mu + k*sigma
	loDur := time.Duration(loSec * float64(time.Second))
	hiDur := time.Duration(hiSec * float64(time.Second))

	// Bound check.
	for i, off := range sch.Offsets {
		if off < loDur || off > hiDur {
			t.Fatalf("offset[%d] = %v outside [%v, %v]", i, off, loDur, hiDur)
		}
	}

	// Mean and std (in seconds) within reasonable tolerance.
	var sum, sumSq float64
	for _, off := range sch.Offsets {
		s := off.Seconds()
		sum += s
		sumSq += s * s
	}
	meanSec := sum / float64(n)
	variance := sumSq/float64(n) - meanSec*meanSec
	if variance < 0 {
		variance = 0
	}
	stdSec := math.Sqrt(variance)

	if math.Abs(meanSec-mu) > 0.2 { // ±0.2s
		t.Errorf("mean = %.3f, want ≈ %.3f (±0.2)", meanSec, mu)
	}
	if math.Abs(stdSec-sigma) > 0.2 {
		t.Errorf("std = %.3f, want ≈ %.3f (±0.2)", stdSec, sigma)
	}
}

func TestBuildSchedule_Uniform_EvenlySpaced(t *testing.T) {
	const n = 10
	const dur = 10.0

	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: n},
		Scenario: config.Scenario{
			Type:    "uniform",
			Uniform: &config.UniformCfg{DurationSec: dur},
		},
	}

	sch, err := BuildSchedule(cfg, 0)
	if err != nil {
		t.Fatalf("BuildSchedule uniform: %v", err)
	}
	if got := sch.Total(); got != n {
		t.Fatalf("Total = %d, want %d", got, n)
	}

	expectedStep := dur / float64(n) // 1.0s per offset

	// Verify ascending order and step.
	for i := 1; i < len(sch.Offsets); i++ {
		got := sch.Offsets[i] - sch.Offsets[i-1]
		want := time.Duration(expectedStep * float64(time.Second))
		// Tolerate 1µs of float-to-duration noise.
		diff := got - want
		if diff < -1*time.Microsecond || diff > 1*time.Microsecond {
			t.Errorf("step[%d] = %v, want ≈ %v", i, got, want)
		}
	}

	if sch.Offsets[0] != 0 {
		t.Errorf("first offset = %v, want 0", sch.Offsets[0])
	}
	wantLast := time.Duration(float64(n-1) * expectedStep * float64(time.Second))
	if sch.Last() != wantLast {
		t.Errorf("last offset = %v, want %v", sch.Last(), wantLast)
	}
}

func TestBuildSchedule_Pessimal_AllWithinWindow(t *testing.T) {
	const n = 50
	const burstMs = 250

	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: n},
		Scenario: config.Scenario{
			Type:     "pessimal",
			Pessimal: &config.PessimalCfg{BurstWindowMs: burstMs},
		},
	}

	sch, err := BuildSchedule(cfg, 0)
	if err != nil {
		t.Fatalf("BuildSchedule pessimal: %v", err)
	}
	if got := sch.Total(); got != n {
		t.Fatalf("Total = %d, want %d", got, n)
	}
	limit := time.Duration(burstMs) * time.Millisecond
	for i, off := range sch.Offsets {
		if off < 0 || off >= limit {
			t.Errorf("offset[%d] = %v outside [0, %v)", i, off, limit)
		}
	}
	// Sorted ascending.
	if !sort.SliceIsSorted(sch.Offsets, func(i, j int) bool { return sch.Offsets[i] < sch.Offsets[j] }) {
		t.Errorf("pessimal offsets not sorted ascending")
	}
}

func TestBuildSchedule_Pessimal_ZeroWindow_AllAtT0(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 5},
		Scenario:    config.Scenario{Type: "pessimal"},
	}
	sch, err := BuildSchedule(cfg, 0)
	if err != nil {
		t.Fatalf("BuildSchedule pessimal: %v", err)
	}
	for i, off := range sch.Offsets {
		if off != 0 {
			t.Errorf("offset[%d] = %v, want 0", i, off)
		}
	}
}

func TestBuildSchedule_CoAStorm_EmptySchedule(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 100},
		Scenario:    config.Scenario{Type: "coa_storm"},
	}
	sch, err := BuildSchedule(cfg, 0)
	if err != nil {
		t.Fatalf("BuildSchedule coa_storm: %v", err)
	}
	if got := sch.Total(); got != 0 {
		t.Errorf("coa_storm Total = %d, want 0", got)
	}
}

func TestBuildSchedule_UnknownType(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 1},
		Scenario:    config.Scenario{Type: "weird"},
	}
	if _, err := BuildSchedule(cfg, 0); err == nil {
		t.Fatalf("BuildSchedule unknown type: want error")
	}
}

func TestBuildSchedule_ColdStartMissingSubBlock(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 1},
		Scenario:    config.Scenario{Type: "cold_start"},
	}
	if _, err := BuildSchedule(cfg, 0); err == nil {
		t.Fatalf("expected error for missing cold_start block")
	}
}

func TestBuildSchedule_NilConfig(t *testing.T) {
	if _, err := BuildSchedule(nil, 0); err == nil {
		t.Fatalf("expected error for nil config")
	}
}

func TestBuildSchedule_Uniform_N1(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 1},
		Scenario: config.Scenario{
			Type:    "uniform",
			Uniform: &config.UniformCfg{DurationSec: 10},
		},
	}
	sch, err := BuildSchedule(cfg, 0)
	if err != nil {
		t.Fatalf("BuildSchedule: %v", err)
	}
	if sch.Total() != 1 || sch.Offsets[0] != 0 {
		t.Errorf("uniform N=1: %v", sch.Offsets)
	}
}
