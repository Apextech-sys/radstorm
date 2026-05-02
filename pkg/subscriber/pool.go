// Package subscriber — Pool that owns the full set of Subscribers and
// maintains O(1) lookup indexes.
//
// Purpose:
//
//	The scenario driver constructs a Pool once at startup, sized to
//	cfg.Subscribers.Count. Indexes by username, acct-session-id, and
//	framed-ip-address are built eagerly so the server-listener
//	(slice 2D) can dispatch inbound CoA / Disconnect packets in
//	constant time, regardless of pool size.
//
// Related files:
//   - pkg/subscriber/subscriber.go (Subscriber type stored in the pool)
//   - pkg/subscriber/lookup.go     (BuildLookup wraps Pool methods for the listener)
//   - pkg/scenario                 (Wave 3 caller — feeds the pool to the activator)
//
// Briefing: .orchestration/briefings/2b-subscriber.md
//
// Contract: Public — Pool, NewPool, Get, LookupBy*, Range, Lookup,
// SubscriberTarget. The SubscriberTarget interface is duck-typed across
// packages: pkg/server consumes a Pool through it without an import.
package subscriber

import (
	"sync"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// SubscriberTarget is the duck-typed contract that the server listener
// (slice 2D pkg/server) consumes when dispatching an inbound CoA or
// Disconnect packet. Subscriber satisfies this implicitly — defined
// here so the Pool's Lookup method has a stable return type.
//
// The OnCoA / OnDisconnect callbacks are concurrency-safe (the
// listener and the subscriber's own Run goroutine may invoke them
// in any order).
type SubscriberTarget interface {
	ID() uint32
	Username() string
	OnCoA(*radius.Packet)
	OnDisconnect(*radius.Packet)
}

// Pool holds every Subscriber created for a scenario plus three lookup
// indexes used by the server listener. Indexes are populated at
// construction time — Subscribers are not added or removed during a
// run (the FSM mutates state, not membership).
//
// Read-mostly access pattern: a single RWMutex protects all indexes
// because lookups are concurrent with Subscriber.Run goroutines but
// the maps themselves are read-only after NewPool returns.
type Pool struct {
	cfg *config.Config
	mu  sync.RWMutex
	all []*Subscriber

	byID        map[uint32]*Subscriber
	byUsername  map[string]*Subscriber
	bySessionID map[string]*Subscriber
	byFramedIP  map[string]*Subscriber
}

// NewPool constructs a Pool with one Subscriber per `cfg.Subscribers.Count`
// entry. Credentials are recycled by index modulo if the credential
// slice is shorter than Count (with one Subscriber per credential being
// the typical configuration). Returns an empty pool with all maps
// non-nil if Count is zero.
//
// Safe to call concurrently with any other code, but the pool itself
// is intended to be constructed once on the orchestrator goroutine.
func NewPool(creds []config.Credential, cfg *config.Config) *Pool {
	p := &Pool{
		cfg:         cfg,
		byID:        make(map[uint32]*Subscriber),
		byUsername:  make(map[string]*Subscriber),
		bySessionID: make(map[string]*Subscriber),
		byFramedIP:  make(map[string]*Subscriber),
	}

	if cfg == nil || cfg.Subscribers.Count <= 0 || len(creds) == 0 {
		return p
	}

	n := cfg.Subscribers.Count
	p.all = make([]*Subscriber, 0, n)

	for i := 0; i < n; i++ {
		cred := creds[i%len(creds)]
		// Synthesise a unique username when the credential pool is shorter
		// than the subscriber count (avoid collisions in byUsername).
		if i >= len(creds) {
			cred.Username = formatRecycledUsername(cred.Username, i)
		}
		s := New(uint32(i), cred, cfg)
		p.all = append(p.all, s)
		p.byID[s.id] = s
		p.byUsername[s.credential.Username] = s
		p.bySessionID[s.sessionID] = s
		p.byFramedIP[s.framedIP.String()] = s
	}

	return p
}

// formatRecycledUsername produces a deterministic unique username when
// the credentials file is reused across subscribers. The original
// username is preserved as a prefix so it remains greppable.
func formatRecycledUsername(base string, idx int) string {
	// Avoid fmt for this hot path.
	out := make([]byte, 0, len(base)+12)
	out = append(out, base...)
	out = append(out, '+')
	// Append decimal idx.
	if idx == 0 {
		out = append(out, '0')
	} else {
		var digits [12]byte
		di := len(digits)
		for idx > 0 {
			di--
			digits[di] = '0' + byte(idx%10)
			idx /= 10
		}
		out = append(out, digits[di:]...)
	}
	return string(out)
}

// Len returns the number of Subscribers in the pool.
func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.all)
}

// Get returns the Subscriber with the given id, or nil if absent.
func (p *Pool) Get(id uint32) *Subscriber {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byID[id]
}

// LookupByUsername returns the Subscriber whose User-Name matches u,
// or nil if absent.
func (p *Pool) LookupByUsername(u string) *Subscriber {
	if u == "" {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byUsername[u]
}

// LookupBySessionID returns the Subscriber whose Acct-Session-Id matches
// s, or nil if absent.
func (p *Pool) LookupBySessionID(s string) *Subscriber {
	if s == "" {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bySessionID[s]
}

// LookupByFramedIP returns the Subscriber whose Framed-IP-Address matches
// ip (formatted as a dotted-quad string), or nil if absent.
func (p *Pool) LookupByFramedIP(ip string) *Subscriber {
	if ip == "" {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byFramedIP[ip]
}

// Range invokes fn for each Subscriber. Iteration order is the order in
// which Subscribers were added to the pool (i.e. ascending ID). fn must
// not call Range or any mutating Pool method (there are none today, but
// future-proof against re-entrancy bugs).
func (p *Pool) Range(fn func(*Subscriber)) {
	p.mu.RLock()
	subs := p.all
	p.mu.RUnlock()
	for _, s := range subs {
		fn(s)
	}
}

// Lookup is the high-level dispatcher used by the server listener. It
// inspects the inbound packet's attributes in priority order
// (Acct-Session-Id, then User-Name, then Framed-IP-Address) and returns
// the first matching SubscriberTarget. The boolean is false if no match.
//
// Returning the SubscriberTarget interface (not *Subscriber) keeps the
// import direction one-way: pkg/server consumes Pool through this
// interface without depending on the concrete Subscriber struct.
func (p *Pool) Lookup(packet *radius.Packet) (SubscriberTarget, bool) {
	if packet == nil {
		return nil, false
	}

	// 1. Acct-Session-Id (most specific).
	if s := packet.Attributes.GetString(radius.AttrAcctSessionID); s != "" {
		if sub := p.LookupBySessionID(s); sub != nil {
			return sub, true
		}
	}
	// 2. User-Name.
	if u := packet.Attributes.GetString(radius.AttrUserName); u != "" {
		if sub := p.LookupByUsername(u); sub != nil {
			return sub, true
		}
	}
	// 3. Framed-IP-Address (least specific — multiple subscribers
	//    could legitimately share a NAT pool address; we return the
	//    first registered).
	if ip, ok := packet.Attributes.GetIPv4(radius.AttrFramedIPAddress); ok {
		if sub := p.LookupByFramedIP(ip.String()); sub != nil {
			return sub, true
		}
	}
	return nil, false
}
