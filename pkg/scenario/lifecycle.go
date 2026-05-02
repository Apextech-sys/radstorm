// Package scenario — lifecycle phases of a scenario run.
//
// Purpose:
//
//	A scenario run progresses through four well-defined phases. The
//	Phase enum is observable to consumers via the progress writer
//	(progress.jsonl) and is what the API server exposes through SSE.
//
//	  Warmup   → bind sockets, verify rig is reachable
//	  Ramp     → execute the activation schedule
//	  Drain    → stop new activations, wait for in-flight subs
//	  Finalize → flush collector, write summary, stop listener/engine
//
// Related files:
//   - pkg/scenario/runner.go    (drives the transitions)
//   - pkg/scenario/progress.go  (emits per-second snapshots labelled by Phase)
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
//
// Contract: Public — Phase string values appear in progress.jsonl and
// must stay stable for downstream consumers (REST API, frontend chart).
package scenario

import "fmt"

// Phase is one of the four lifecycle stages of a scenario run.
type Phase string

// Phase values. The string forms appear verbatim in progress.jsonl
// (the "phase" field) and on the wire to the frontend.
const (
	PhaseWarmup   Phase = "warmup"
	PhaseRamp     Phase = "ramp"
	PhaseDrain    Phase = "drain"
	PhaseFinalize Phase = "finalize"

	// PhaseDone is reported after Finalize completes. The runner does
	// not stay in this phase; it's emitted as the very last progress
	// snapshot so consumers know the run is finished.
	PhaseDone Phase = "done"
)

// Order returns the canonical phase ordering as a slice.
func phaseOrder() []Phase {
	return []Phase{PhaseWarmup, PhaseRamp, PhaseDrain, PhaseFinalize, PhaseDone}
}

// nextPhase returns the phase that follows p in the canonical order. The
// last phase returns itself (PhaseDone is terminal).
func nextPhase(p Phase) Phase {
	order := phaseOrder()
	for i, q := range order {
		if q == p && i+1 < len(order) {
			return order[i+1]
		}
	}
	return PhaseDone
}

// validateTransition reports whether moving from old → new respects the
// canonical order. Used by the runner so a programming error (e.g.
// transitioning from Warmup straight to Finalize) panics in tests.
func validateTransition(oldP, newP Phase) error {
	if oldP == newP {
		return nil
	}
	order := phaseOrder()
	oldIdx, newIdx := -1, -1
	for i, p := range order {
		if p == oldP {
			oldIdx = i
		}
		if p == newP {
			newIdx = i
		}
	}
	if oldIdx == -1 {
		return fmt.Errorf("scenario: unknown old phase %q", oldP)
	}
	if newIdx == -1 {
		return fmt.Errorf("scenario: unknown new phase %q", newP)
	}
	if newIdx != oldIdx+1 {
		return fmt.Errorf("scenario: invalid phase transition %s → %s (must be canonical-order step)", oldP, newP)
	}
	return nil
}
