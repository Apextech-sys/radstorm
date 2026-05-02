// Package server — RFC 5176 CoA + Disconnect listener for radstorm.
//
// Purpose:
//
//	Defines the small interfaces the listener needs to find the virtual
//	subscriber a server-initiated CoA-Request or Disconnect-Request is
//	targeting, and to dispatch the decoded packet onto that subscriber.
//	Kept in its own file because pkg/subscriber satisfies these interfaces
//	via duck typing — no import cycle.
//
// Related files:
//   - pkg/server/listener.go (UDP listen + packet routing)
//   - pkg/server/handler.go  (CoA + Disconnect validation/response)
//   - pkg/subscriber          (Pool implements SubscriberLookup; Subscriber
//     implements SubscriberTarget — wired in Wave 3)
//   - pkg/collector           (Collector satisfies the Collector interface here)
//
// Briefing: .orchestration/briefings/2c-server-listener.md
//
// Contract: Public — these interfaces are the duck-typed seam between the
// server listener, the subscriber pool, and the collector. Renaming or
// changing method signatures is a breaking change for those packages.
package server

import (
	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// SubscriberTarget is the subset of a virtual subscriber the listener
// invokes when a valid CoA-Request or Disconnect-Request arrives for it.
//
// pkg/subscriber.Subscriber satisfies this interface in Wave 3 with the
// FSM transitions wired into OnCoA / OnDisconnect.
type SubscriberTarget interface {
	// ID is the harness-internal subscriber identifier (also used as
	// SubscriberID in events). uint32 to match events.Event.SubscriberID.
	ID() uint32

	// Username returns the User-Name the subscriber authenticated with.
	// Used in event tags so operators can correlate a CoA event to a
	// real session in their reports.
	Username() string

	// OnCoA is invoked AFTER the listener has sent CoA-ACK on the wire.
	// The decoded request packet is supplied so the subscriber can apply
	// rate-plan changes / log VSAs / drive its FSM as appropriate.
	//
	// The listener calls this on a fresh goroutine — implementations need
	// not return quickly. The packet pointer is read-only from the
	// listener's perspective; implementations must not mutate it.
	OnCoA(req *radius.Packet)

	// OnDisconnect is the sibling of OnCoA for Disconnect-Request.
	// Same goroutine + ownership rules.
	OnDisconnect(req *radius.Packet)
}

// SubscriberLookup resolves an inbound dynamic-authorization packet to
// the targeted subscriber. The listener does not know how subscribers are
// identified (User-Name, Acct-Session-Id, Framed-IP-Address, vendor
// session-id VSA, …) — that policy lives in the pool implementation.
//
// Returns (target, true) if a subscriber matched; (nil, false) if no
// session context was found (RFC 5176 §3.5 error 503).
type SubscriberLookup interface {
	Lookup(req *radius.Packet) (SubscriberTarget, bool)
}

// Collector is the subset of pkg/collector.Collector the listener uses.
// Defined as an interface so tests can swap in a recorder without spinning
// up Parquet writers. pkg/collector.Collector satisfies this interface
// directly via its Submit(events.Event) method.
type Collector interface {
	Submit(e events.Event)
}
