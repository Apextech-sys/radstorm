// Package subscriber — server-listener lookup adapter.
//
// Purpose:
//
//	Exposes a small adapter around Pool.Lookup that the server listener
//	(slice 2D pkg/server) consumes via duck-typing. The listener does
//	NOT import this package; it declares its own SubscriberLookup
//	interface that the function returned here satisfies.
//
// Related files:
//   - pkg/subscriber/pool.go   (Pool.Lookup is the underlying call)
//   - pkg/server               (consumer; declares its own lookup interface)
//   - .orchestration/briefings/2b-subscriber.md
//
// Briefing: .orchestration/briefings/2b-subscriber.md
//
// Contract: BuildLookup is part of the public API. The returned closure
// signature `func(*radius.Packet) SubscriberTarget` is the one the
// server listener accepts (matched structurally, no shared interface).
package subscriber

import (
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// LookupFunc is the closure type returned by BuildLookup. The server
// listener calls it on every inbound CoA / Disconnect packet to find
// the target subscriber. Returns nil when no subscriber matches; the
// listener responds with Error-Cause = 503 (Session-Context-Not-Found)
// in that case (per docs/PROTOCOL.md).
type LookupFunc func(*radius.Packet) SubscriberTarget

// BuildLookup returns a LookupFunc bound to the given pool. The
// returned closure is safe for concurrent use (it dispatches into
// Pool.Lookup, which holds an RWMutex internally).
//
// This is the seam between pkg/subscriber and pkg/server: the listener
// does not import pkg/subscriber directly; it accepts a value of the
// shape `func(*radius.Packet) SubscriberTarget` and invokes it on each
// inbound DAE packet.
func BuildLookup(p *Pool) LookupFunc {
	if p == nil {
		return func(*radius.Packet) SubscriberTarget { return nil }
	}
	return func(pkt *radius.Packet) SubscriberTarget {
		t, ok := p.Lookup(pkt)
		if !ok {
			return nil
		}
		return t
	}
}
