// Package collector — race-detector-aware test toggles.
//
// Purpose:
//   The 1M-event performance gate runs at full speed when the race
//   detector is OFF. This file's build constraint enables the gate.
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — test-only.

//go:build !race

package collector

const raceEnabled = false
