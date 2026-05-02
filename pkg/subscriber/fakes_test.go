// Package subscriber — test doubles for Sender and Collector.
//
// Purpose:
//
//	The FSM tests must not perform real I/O. fakeSender returns scripted
//	replies (or scripted errors), and fakeCollector records every event
//	and outcome submitted so tests can assert against them.
//
// Related files:
//   - pkg/subscriber/subscriber.go (Run consumes Sender + Collector)
//   - pkg/io/io.go                 (canonical Sender / RetransmitPolicy / SendResult)
//
// Briefing: .orchestration/briefings/2b-subscriber.md
//
// Contract: Test-only — types here are not exported.
package subscriber

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// fakeAddr is a simple net.Addr used in fake send results.
type fakeAddr struct{ s string }

func (f fakeAddr) Network() string { return "udp" }
func (f fakeAddr) String() string  { return f.s }

// scriptedReply describes a single fake send outcome.
//
// io.SendResult does not carry an in-band Err field — terminal errors
// are returned as the second value of Send. err here therefore always
// becomes the returned error and (nil, err) is the response.
type scriptedReply struct {
	// replyCode is the RADIUS code in the synthetic reply (Access-Accept,
	// Access-Reject, Accounting-Response). Zero means "no reply" (the
	// sender returns err instead).
	replyCode radius.Code

	// retransmits is reported back in SendResult.RetransmitN.
	retransmits int

	// err is returned as Send's error value (paired with a nil result).
	// Retained for readability with the previous wantHardErr flag.
	err error

	// wantHardErr is retained for back-compat with existing tests; it has
	// no effect now (any non-nil err always returns (nil, err)).
	wantHardErr bool

	// latency is reported as SendResult.LatencyUs (microseconds), useful
	// for emitReplyEvent assertions.
	latency time.Duration
}

// fakeSender returns scripted replies to consecutive Send calls.
type fakeSender struct {
	mu      sync.Mutex
	scripts []scriptedReply
	calls   []sendCall
}

type sendCall struct {
	dst    net.Addr
	policy RetransmitPolicy
	subID  uint32
	// built captures the packet the FSM produced via the build closure.
	built *radius.Packet
}

func newFakeSender(scripts ...scriptedReply) *fakeSender {
	return &fakeSender{scripts: scripts}
}

func (f *fakeSender) Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.scripts) == 0 {
		return nil, errors.New("fakeSender: out of scripts")
	}
	script := f.scripts[0]
	f.scripts = f.scripts[1:]

	// Always invoke build once to exercise the construction path.
	pkt, err := build(7)
	if err != nil {
		return nil, err
	}

	f.calls = append(f.calls, sendCall{dst: dst, policy: policy, subID: subID, built: pkt})

	if script.err != nil {
		return nil, script.err
	}

	res := &SendResult{
		RetransmitN: script.retransmits,
		LocalAddr:   fakeAddr{s: "127.0.0.1:0"},
		LatencyUs:   script.latency.Microseconds(),
		Identifier:  pkt.Identifier,
	}

	if script.replyCode != 0 {
		// Use the FSM-built request's identifier so the event log
		// correlates request and reply.
		res.Reply = &radius.Packet{
			Code:       script.replyCode,
			Identifier: pkt.Identifier,
		}
	}
	return res, nil
}

// callCount returns how many times Send has been invoked so far.
func (f *fakeSender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// recordedCall returns the i-th sendCall observed (panics if out of range).
func (f *fakeSender) recordedCall(i int) sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

// fakeCollector accumulates submitted events and outcomes for assertion.
type fakeCollector struct {
	mu       sync.Mutex
	events   []events.Event
	outcomes []events.SubscriberOutcome
}

func newFakeCollector() *fakeCollector {
	return &fakeCollector{}
}

func (c *fakeCollector) Submit(e events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *fakeCollector) SubmitOutcome(o events.SubscriberOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outcomes = append(c.outcomes, o)
}

func (c *fakeCollector) snapshotEvents() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.events))
	copy(out, c.events)
	return out
}

func (c *fakeCollector) snapshotOutcomes() []events.SubscriberOutcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.SubscriberOutcome, len(c.outcomes))
	copy(out, c.outcomes)
	return out
}

// hasEventOfType returns true if any event matches the given category +
// type pair.
func (c *fakeCollector) hasEventOfType(cat, typ string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.Category == cat && e.EventType == typ {
			return true
		}
	}
	return false
}

// stateChangedTo returns true if any state_changed event recorded the
// given new state.
func (c *fakeCollector) stateChangedTo(target string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.EventType == events.EventTypeStateChanged && e.State == target {
			return true
		}
	}
	return false
}
