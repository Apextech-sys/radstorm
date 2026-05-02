// Package io — RetransmitPolicy timeoutFor tests.
//
// Purpose:
//
//	Exercises BackoffConstant / BackoffLinear / BackoffExponential curves
//	and the InitialTimeout-vs-backoff handoff so callers can rely on the
//	per-attempt timeouts they think they configured.
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetransmitPolicy_BackoffCurves(t *testing.T) {
	base := 100 * time.Millisecond
	cases := []struct {
		name     string
		backoff  Backoff
		attempts []time.Duration
	}{
		{
			"constant",
			BackoffConstant,
			[]time.Duration{0, base, base, base},
		},
		{
			"linear",
			BackoffLinear,
			[]time.Duration{0, base, 2 * base, 3 * base},
		},
		{
			"exponential",
			BackoffExponential,
			[]time.Duration{0, base, 2 * base, 4 * base},
		},
	}

	initial := 250 * time.Millisecond
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := RetransmitPolicy{
				InitialTimeout: initial,
				MaxRetries:     3,
				Backoff:        c.backoff,
				BackoffBase:    base,
			}
			// Attempt 0 is always the InitialTimeout.
			assert.Equal(t, initial, p.timeoutFor(0))
			// Attempts 1+ follow the curve.
			for n := 1; n < len(c.attempts); n++ {
				got := p.timeoutFor(n)
				assert.Equal(t, c.attempts[n], got, "attempt %d", n)
			}
		})
	}
}
