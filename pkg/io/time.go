// Package io — small time helper.
//
// Purpose:
//
//	Tiny wrapper around time.Now().UnixNano() defined as a function so
//	the receiver and sender can call it without each individually
//	importing "time" (and so tests could override at the variable level
//	if needed). The package as a whole still imports time wherever it
//	uses durations.
//
// Related files:
//   - pkg/io/receiver.go (calls timeNowUnixNano)
//   - pkg/io/sender.go   (uses time durations directly via "time")
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: internal — package-private helper.
package io

import "time"

func timeNowUnixNano() int64 { return time.Now().UnixNano() }
