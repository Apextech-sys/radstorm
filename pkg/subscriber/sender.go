// Package subscriber — Sender interface and supporting types consumed by
// the FSM but implemented by pkg/io (slice 2A, separate worktree).
//
// Purpose:
//
//	Defines the duck-typed Sender interface and the RetransmitPolicy /
//	SendResult shapes the FSM passes to it. Slice 2A implements the
//	concrete I/O engine; this package compiles independently against
//	these signatures and substitutes a fake Sender in tests.
//
// Related files:
//   - pkg/subscriber/subscriber.go (calls Sender.Send during Run)
//   - pkg/subscriber/fakes_test.go (test double)
//   - .orchestration/briefings/2b-subscriber.md (signature pinned here)
//
// Briefing: .orchestration/briefings/2b-subscriber.md
//
// Contract: Sender / RetransmitPolicy / SendResult signatures are a
// coordination point with slice 2A. Renames or shape changes require
// updating both packages — pkg/io.Engine.Send must satisfy this
// interface and consume these types in the integration wave.
package subscriber

import (
	"context"
	"net"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// RetransmitPolicy governs how the I/O layer retries a single request.
// Values mirror the [retransmit] block of the scenario config (see
// pkg/config.Retransmit).
//
// Fresh Identifiers must be allocated on each retry per RFC 2865 §3 to
// avoid colliding with a stale-but-still-in-flight original — see
// docs/PROTOCOL.md "Retransmit policy".
type RetransmitPolicy struct {
	// InitialTimeout is the upper bound for waiting on the FIRST reply
	// before declaring a timeout and (if MaxRetries > 0) retransmitting.
	InitialTimeout time.Duration

	// MaxRetries is the number of retransmits allowed AFTER the original
	// send (so total send attempts = MaxRetries + 1). 0 disables retries.
	MaxRetries int

	// Backoff is the strategy used to compute the wait between attempts.
	// "exponential" (default) doubles BackoffBase each retry; "fixed"
	// uses BackoffBase for every retry.
	Backoff string

	// BackoffBase is the unit step for the backoff calculation.
	BackoffBase time.Duration
}

// SendResult is what Sender.Send returns: either a matched reply or a
// terminal failure after exhausting retries. The FSM inspects Reply.Code
// to decide the next state and reads Retransmits to populate the
// per-subscriber outcome.
//
// Reply is the parsed reply packet (Access-Accept, Access-Reject, or
// Accounting-Response); nil when Err is non-nil.
//
// Retransmits is the count of retransmits actually performed (0 = first
// attempt succeeded). Carried into SubscriberOutcome.AuthRetransmits /
// AcctRetransmits.
//
// LocalAddr and RemoteAddr are the addresses the request was sent
// from/to, recorded for the event log.
//
// FirstSentAt is the monotonic-anchored wall-clock instant of the
// original transmission. ReplyAt is when the reply was matched. The
// difference is the per-attempt latency reported in the event log;
// the FSM passes it through to the collector.
//
// Err is non-nil on terminal failure (timeout exhausted, send error,
// context canceled). The FSM treats this as a transition to
// auth_failed / acct_failed depending on the current state.
type SendResult struct {
	Reply       *radius.Packet
	ReplyBytes  []byte
	Retransmits int
	LocalAddr   net.Addr
	RemoteAddr  net.Addr
	FirstSentAt time.Time
	ReplyAt     time.Time
	Err         error
}

// Latency returns the wall-clock duration from FirstSentAt to ReplyAt,
// or 0 if either is zero (e.g. on a terminal error).
func (r *SendResult) Latency() time.Duration {
	if r == nil || r.FirstSentAt.IsZero() || r.ReplyAt.IsZero() {
		return 0
	}
	return r.ReplyAt.Sub(r.FirstSentAt)
}

// Sender is the I/O surface the FSM consumes. The build closure produces
// the next attempt's packet given a freshly-allocated 8-bit Identifier;
// the implementation calls it once per send attempt so each retransmit
// gets a fresh ID + fresh request authenticator (per docs/PROTOCOL.md).
//
// dst is the target address (auth or accounting). policy controls the
// retry behaviour. subID is forwarded to the I/O layer purely for
// logging/diagnostics correlation; it is NOT serialised on the wire.
type Sender interface {
	Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error)
}

// PolicyFromConfig converts the user-facing config block into a
// RetransmitPolicy with defaults applied per docs/PROTOCOL.md
// "Retransmit policy".
func PolicyFromConfig(initialTimeoutMs, maxRetries int, backoff string, backoffBaseMs int) RetransmitPolicy {
	const (
		defaultInitialTimeout = 5 * time.Second
		defaultMaxRetries     = 3
		defaultBackoff        = "exponential"
		defaultBackoffBase    = 1 * time.Second
	)
	p := RetransmitPolicy{
		InitialTimeout: time.Duration(initialTimeoutMs) * time.Millisecond,
		MaxRetries:     maxRetries,
		Backoff:        backoff,
		BackoffBase:    time.Duration(backoffBaseMs) * time.Millisecond,
	}
	if p.InitialTimeout <= 0 {
		p.InitialTimeout = defaultInitialTimeout
	}
	if p.MaxRetries <= 0 {
		p.MaxRetries = defaultMaxRetries
	}
	if p.Backoff == "" {
		p.Backoff = defaultBackoff
	}
	if p.BackoffBase <= 0 {
		p.BackoffBase = defaultBackoffBase
	}
	return p
}
