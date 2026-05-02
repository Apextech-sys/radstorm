// Package events — monotonic clock helper for event timestamps.
//
// Purpose:
//   The Event struct carries a monotonic-nanosecond field used for latency
//   math (subtracting two MonotonicNs values is wall-clock-jump-safe). Go's
//   stdlib hides the raw monotonic reading, so we anchor at process start
//   and report deltas from that anchor.
//
// Related files:
//   - pkg/events/event.go (constructors call monotonicNow)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — package-private helper.
package events

import "time"

// processStart is captured once at package init. time.Since on the same
// time.Time value returns the monotonic delta even if the wall clock has
// jumped, which is exactly the property we want for latency math.
var processStart = time.Now()

// monotonicNow returns nanoseconds since the process-start anchor.
// Two readings can be subtracted to get a monotonic duration in ns.
func monotonicNow() int64 {
	return time.Since(processStart).Nanoseconds()
}
