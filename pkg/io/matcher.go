// Package io — outstanding-request correlation table.
//
// Purpose:
//
//	The ReplyMatcher is the rendezvous between Sender (which registers
//	an outstanding request keyed by (localAddr, identifier)) and Receiver
//	(which delivers parsed reply packets back to the matching Sender via a
//	channel). Duplicate replies (same key + matching request authenticator
//	but the request was already answered) are detected and emitted as
//	events but not re-delivered. Authenticator validation lives here so
//	the Sender can rely on the matched reply being verified.
//
// Related files:
//   - pkg/io/sender.go   (Register, AwaitReply, Cancel)
//   - pkg/io/receiver.go (Deliver)
//   - pkg/radius/packet.go (ValidateResponseAuthenticator)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: internal — exposed in this package for sender.go and the
// receiver. Tests verify duplicate-detection and bad-auth rejection.
package io

import (
	"net"
	"sync"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// matchKey is the lookup key for outstanding requests. localAddr is the
// stringified UDPAddr we sent FROM (which is what the reply is addressed
// TO).
type matchKey struct {
	LocalAddr  string
	Identifier uint8
}

// inflight is a single outstanding request awaiting a reply.
type inflight struct {
	// Request is the original request packet so the matcher can validate
	// the reply's Response Authenticator against its RequestAuthenticator.
	Request *radius.Packet

	// Reply is delivered here on first match. Buffered (cap 1) so
	// Deliver never blocks even if the waiting goroutine has gone away
	// (cancelled).
	Reply chan replyDelivery

	// SubID is the subscriber ID for event emission of duplicates.
	SubID uint32

	// done is closed once the first reply has been delivered or the
	// inflight has been cancelled.
	done chan struct{}

	doneOnce sync.Once
}

// completed is bookkeeping for a recently-completed request. Kept around
// so that a late-arriving second reply (e.g. the original after we
// already retransmitted, or an over-eager server) is recognised as a
// duplicate rather than as unmatched / silently re-driving the FSM.
type completed struct {
	requestAuthenticator [16]byte
	subID                uint32
	completedAt          time.Time
}

// replyDelivery wraps the parsed reply with its arrival metadata so the
// Sender can emit a fully-populated reply event.
type replyDelivery struct {
	Packet     *radius.Packet
	WireBytes  []byte
	RemoteAddr net.Addr
}

// completedTTL is how long the matcher remembers a recently-completed
// request for the purpose of duplicate detection. After this, a late
// reply for the same ID is reported as unmatched.
//
// Default 30s — well above any sane retransmit policy timeout, well
// below any memory pressure concern. Exposed via package-level var so
// tests can shorten it.
var completedTTL = 30 * time.Second

// ReplyMatcher tracks outstanding requests and routes inbound replies.
// Safe for concurrent use.
type ReplyMatcher struct {
	mu          sync.Mutex
	pending     map[matchKey]*inflight
	recentDone  map[matchKey]completed
	collector   Collector
	gcThreshold int // size of recentDone above which we sweep on insert
}

// NewReplyMatcher constructs an empty matcher.
func NewReplyMatcher(c Collector) *ReplyMatcher {
	if c == nil {
		c = nopCollector{}
	}
	return &ReplyMatcher{
		pending:     make(map[matchKey]*inflight),
		recentDone:  make(map[matchKey]completed),
		collector:   c,
		gcThreshold: 1024,
	}
}

// Register inserts an outstanding request into the table. The returned
// channel receives the matching reply once Deliver is called for it; it
// is closed via Cancel if the caller gives up first. Re-registering an
// existing key replaces the previous inflight (the previous waiter, if
// any, will never be delivered to — Sender always Cancels before
// Register-ing a new ID for retransmits, but defensive cleanup is here).
func (m *ReplyMatcher) Register(localAddr net.Addr, identifier uint8, req *radius.Packet, subID uint32) <-chan replyDelivery {
	k := matchKey{LocalAddr: localAddr.String(), Identifier: identifier}
	ifl := &inflight{
		Request: req,
		Reply:   make(chan replyDelivery, 1),
		SubID:   subID,
		done:    make(chan struct{}),
	}
	m.mu.Lock()
	if old, ok := m.pending[k]; ok {
		old.doneOnce.Do(func() { close(old.done) })
	}
	m.pending[k] = ifl
	// Re-using a key — the previous "completed" entry for this key (if
	// any) is now stale because the ID has been recycled.
	delete(m.recentDone, k)
	m.mu.Unlock()
	return ifl.Reply
}

// Cancel removes the entry for (localAddr, identifier). Safe to call even
// after Deliver fired (no-op).
func (m *ReplyMatcher) Cancel(localAddr net.Addr, identifier uint8) {
	k := matchKey{LocalAddr: localAddr.String(), Identifier: identifier}
	m.mu.Lock()
	if ifl, ok := m.pending[k]; ok {
		delete(m.pending, k)
		ifl.doneOnce.Do(func() { close(ifl.done) })
	}
	m.mu.Unlock()
}

// Deliver routes a parsed reply to its matching outstanding request:
//   - Looks up by (localAddr, reply.Identifier).
//   - If a matching inflight exists, validates Response Authenticator
//     against the original request's RequestAuthenticator + shared secret;
//     bad auth → drop + emit validation_failed (the inflight is left in
//     place so a subsequent legitimate reply can still match).
//   - On first valid match: removes the inflight, records a "completed"
//     entry (so a duplicate can be recognised), pushes the reply onto
//     the inflight's channel.
//   - On subsequent reply for a recently-completed key whose
//     requestAuthenticator matches: emit duplicate_reply event, return.
//   - Else: emit unmatched_reply.
//
// Returns true if the reply was attributable to a known request (matched
// or duplicate of a matched), false if fully unmatched.
func (m *ReplyMatcher) Deliver(localAddr net.Addr, remoteAddr net.Addr, reply *radius.Packet, wireBytes []byte, secret []byte, offsetMs int64) bool {
	k := matchKey{LocalAddr: localAddr.String(), Identifier: reply.Identifier}

	m.mu.Lock()
	ifl, ok := m.pending[k]
	var dup completed
	dupOk := false
	if !ok {
		dup, dupOk = m.recentDone[k]
	}
	m.mu.Unlock()

	if ok {
		// Pending — validate auth before claiming.
		if !reply.ValidateResponseAuthenticator(ifl.Request, secret, wireBytes) {
			m.collector.Submit(events.NewError(
				ifl.SubID,
				offsetMs,
				events.EventTypeValidationFailed,
				0,
				"response authenticator mismatch",
			))
			return false
		}
		// Claim under lock to avoid two parallel valid replies both
		// thinking they're first.
		m.mu.Lock()
		stillPending, alive := m.pending[k]
		if alive && stillPending == ifl {
			delete(m.pending, k)
			m.recentDone[k] = completed{
				requestAuthenticator: ifl.Request.Authenticator,
				subID:                ifl.SubID,
				completedAt:          time.Now(),
			}
			m.gcLocked()
			m.mu.Unlock()
			ifl.doneOnce.Do(func() { close(ifl.done) })
			select {
			case ifl.Reply <- replyDelivery{Packet: reply, WireBytes: wireBytes, RemoteAddr: remoteAddr}:
			default:
				// Channel cap 1, never previously written under our lock.
			}
			return true
		}
		// Lost a race with another Deliver / Cancel — fall through to
		// duplicate / unmatched logic.
		m.mu.Unlock()
		m.mu.Lock()
		dup, dupOk = m.recentDone[k]
		m.mu.Unlock()
	}

	if dupOk {
		// We have a recently-completed request for this key. Validate
		// against the recorded request authenticator using a synthetic
		// "request" packet so we can reuse ValidateResponseAuthenticator.
		fakeReq := &radius.Packet{Authenticator: dup.requestAuthenticator}
		if reply.ValidateResponseAuthenticator(fakeReq, secret, wireBytes) {
			m.collector.Submit(events.NewDuplicateReply(
				dup.subID,
				offsetMs,
				int8(reply.Code),
				int16(reply.Identifier),
				localAddr.String(),
				remoteAddr.String(),
				int32(len(wireBytes)),
			))
			return true
		}
		// Authenticator doesn't match — fall through to unmatched.
	}

	m.collector.Submit(events.NewUnmatchedReply(
		offsetMs,
		int8(reply.Code),
		int16(reply.Identifier),
		localAddr.String(),
		remoteAddr.String(),
		int32(len(wireBytes)),
	))
	return false
}

// CloseAll cancels every outstanding inflight (called on Engine.Stop).
func (m *ReplyMatcher) CloseAll() {
	m.mu.Lock()
	for k, ifl := range m.pending {
		ifl.doneOnce.Do(func() { close(ifl.done) })
		delete(m.pending, k)
	}
	m.recentDone = make(map[matchKey]completed)
	m.mu.Unlock()
}

// Inflight reports the number of outstanding requests.
func (m *ReplyMatcher) Inflight() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// gcLocked drops stale recentDone entries when the table grows past the
// threshold. Called with m.mu held.
func (m *ReplyMatcher) gcLocked() {
	if len(m.recentDone) < m.gcThreshold {
		return
	}
	cutoff := time.Now().Add(-completedTTL)
	for k, c := range m.recentDone {
		if c.completedAt.Before(cutoff) {
			delete(m.recentDone, k)
		}
	}
}
