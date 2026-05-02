// Package collector — race-detector-aware test toggles (race build).
//
// Purpose:
//   When -race is in effect, raceEnabled flips to true so timing-sensitive
//   gates can relax (race adds 5–10x overhead) while correctness checks
//   still run.
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — test-only.

//go:build race

package collector

const raceEnabled = true
