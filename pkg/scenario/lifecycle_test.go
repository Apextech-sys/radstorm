// Package scenario — tests for Phase enum + transition validation.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package scenario

import "testing"

func TestPhaseOrder(t *testing.T) {
	want := []Phase{PhaseWarmup, PhaseRamp, PhaseDrain, PhaseFinalize, PhaseDone}
	got := phaseOrder()
	if len(got) != len(want) {
		t.Fatalf("phaseOrder len = %d, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("phaseOrder[%d] = %s, want %s", i, got[i], p)
		}
	}
}

func TestNextPhase(t *testing.T) {
	cases := []struct {
		in, want Phase
	}{
		{PhaseWarmup, PhaseRamp},
		{PhaseRamp, PhaseDrain},
		{PhaseDrain, PhaseFinalize},
		{PhaseFinalize, PhaseDone},
		{PhaseDone, PhaseDone}, // terminal
	}
	for _, c := range cases {
		if got := nextPhase(c.in); got != c.want {
			t.Errorf("nextPhase(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestValidateTransition_HappyPath(t *testing.T) {
	cases := [][2]Phase{
		{PhaseWarmup, PhaseRamp},
		{PhaseRamp, PhaseDrain},
		{PhaseDrain, PhaseFinalize},
		{PhaseFinalize, PhaseDone},
		{PhaseRamp, PhaseRamp}, // self-transition allowed
	}
	for _, c := range cases {
		if err := validateTransition(c[0], c[1]); err != nil {
			t.Errorf("validateTransition(%s → %s) = %v, want nil", c[0], c[1], err)
		}
	}
}

func TestValidateTransition_RejectSkipping(t *testing.T) {
	if err := validateTransition(PhaseWarmup, PhaseFinalize); err == nil {
		t.Errorf("expected error skipping Warmup → Finalize")
	}
	if err := validateTransition(PhaseDrain, PhaseRamp); err == nil {
		t.Errorf("expected error reversing Drain → Ramp")
	}
	if err := validateTransition("bogus", PhaseRamp); err == nil {
		t.Errorf("expected error unknown old phase")
	}
	if err := validateTransition(PhaseRamp, "bogus"); err == nil {
		t.Errorf("expected error unknown new phase")
	}
}
