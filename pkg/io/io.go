// Package io — UDP I/O layer for radstorm: socket pool, ID allocator,
// reply matcher, sender with retransmit, receiver.
//
// Purpose:
//
//	Provides the public Engine that ties the I/O sub-components together.
//	The Engine binds UDP sockets across configured source IPs and ports,
//	hands out per-tuple 8-bit RADIUS Identifiers, sends RADIUS requests
//	with retransmit policy, and routes replies back to the waiting caller
//	while routing server-initiated CoA/Disconnect packets to a registered
//	handler.
//
// Related files:
//   - pkg/io/socketpool.go (UDP sockets across configured source IPs)
//   - pkg/io/idalloc.go    (per-tuple 8-bit Identifier bitmap)
//   - pkg/io/matcher.go    (correlation table; matches replies; detects duplicates)
//   - pkg/io/sender.go     (Send with retransmit lifecycle)
//   - pkg/io/receiver.go   (per-socket inbound reader; routes replies and server-initiated)
//   - pkg/radius           (Packet, Encode, Decode, ValidateResponseAuthenticator)
//   - pkg/events           (Event constructors emitted to the Collector)
//   - .orchestration/contracts/event-schema.md (events emitted by this package)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: Public — Engine, Opts, RetransmitPolicy, SendResult,
// ServerHandler, Collector. Used by pkg/subscriber (sender) and pkg/server
// (server handler implementor).
//
// MEASUREMENT FLOOR: this package uses Go's standard net.UDPConn, which
// gives userspace timestamps. Effective measurement precision is bounded
// by kernel scheduling jitter (~50–200µs on a clean Linux box). For sub-
// millisecond precision we would need to switch to raw sockets with
// SO_TIMESTAMPING on a hardware-timestamping-capable NIC; this is a
// deferred design — see docs/decisions/0004-hardware-timestamping.md.
package io

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// Backoff specifies the retransmit backoff curve.
type Backoff int

const (
	// BackoffConstant uses BackoffBase between every retransmit.
	BackoffConstant Backoff = iota
	// BackoffLinear uses BackoffBase * attempt between retransmits.
	BackoffLinear
	// BackoffExponential uses BackoffBase * 2^(attempt-1) between retransmits.
	BackoffExponential
)

// Collector is the local interface the I/O layer submits Events on. The
// concrete implementation in pkg/collector satisfies it. Defined here so
// pkg/io does not import pkg/collector.
type Collector interface {
	Submit(events.Event)
}

// nopCollector is a fallback Collector used when none is provided.
type nopCollector struct{}

func (nopCollector) Submit(events.Event) {}

// ServerHandler is invoked by the receiver when an inbound packet is a
// server-initiated request (CoA-Request / Disconnect-Request). The handler
// owns crafting and sending the ACK/NAK reply (via its own socket) — the
// I/O layer does not respond on behalf of the handler.
type ServerHandler interface {
	HandleServerInitiated(p *radius.Packet, wireBytes []byte, srcAddr net.Addr, localAddr net.Addr)
}

// ServerHandlerFunc adapts a plain function to ServerHandler.
type ServerHandlerFunc func(p *radius.Packet, wireBytes []byte, srcAddr net.Addr, localAddr net.Addr)

// HandleServerInitiated implements ServerHandler.
func (f ServerHandlerFunc) HandleServerInitiated(p *radius.Packet, wireBytes []byte, srcAddr net.Addr, localAddr net.Addr) {
	f(p, wireBytes, srcAddr, localAddr)
}

// Opts configures an Engine.
type Opts struct {
	// SourceIPs is the list of local IP addresses to bind to. Each (IP,
	// port) pair becomes a UDP socket. At least one IP is required. Use
	// "0.0.0.0" / "::" to bind to all interfaces.
	SourceIPs []net.IP

	// PortRangeLo and PortRangeHi define the inclusive UDP port range to
	// bind on each source IP. PortRangeLo == PortRangeHi == 0 lets the
	// kernel choose a single ephemeral port per source IP.
	PortRangeLo int
	PortRangeHi int

	// Secret is the RADIUS shared secret used for response authenticator
	// validation in the receiver. Required.
	Secret []byte

	// Collector receives Events emitted by the I/O layer. Optional; if
	// nil, events are dropped on the floor.
	Collector Collector

	// Handler receives server-initiated packets. Optional; if nil,
	// inbound CoA / Disconnect requests are dropped.
	Handler ServerHandler

	// ReadBufBytes is the per-read receive buffer. Default 4096
	// (RADIUS max packet length).
	ReadBufBytes int

	// AllocBackoffStep is how long Send waits between identifier
	// allocation retries when all 256 IDs for a chosen tuple are in
	// flight. Default 5ms.
	AllocBackoffStep time.Duration

	// AllocMaxWait is the upper bound Send will spend trying to allocate
	// an identifier across all available tuples before returning
	// ErrIdentifierExhausted. Default 100ms.
	AllocMaxWait time.Duration
}

// RetransmitPolicy configures Sender retransmit behaviour.
type RetransmitPolicy struct {
	// InitialTimeout is how long to wait for a reply on the original
	// transmission. Required (>0).
	InitialTimeout time.Duration
	// MaxRetries is the maximum number of retransmits to attempt after
	// the original transmission times out. 0 means no retransmits.
	MaxRetries int
	// Backoff selects the backoff curve for retransmit delays. The
	// per-attempt timeout is (BackoffBase * curve). Use the same
	// timeout for the inflight wait per attempt.
	Backoff Backoff
	// BackoffBase is the unit duration used by the Backoff curve.
	BackoffBase time.Duration
}

// timeoutFor returns the per-attempt timeout (and backoff sleep) for
// retransmit attempt n where n=0 is the original send.
func (p RetransmitPolicy) timeoutFor(n int) time.Duration {
	if n == 0 {
		return p.InitialTimeout
	}
	switch p.Backoff {
	case BackoffLinear:
		return p.BackoffBase * time.Duration(n)
	case BackoffExponential:
		return p.BackoffBase * (1 << (n - 1))
	default:
		return p.BackoffBase
	}
}

// SendResult is the outcome of a successful Send.
type SendResult struct {
	// Reply is the matched reply packet; its Authenticator has been
	// validated and the wire bytes have been parsed.
	Reply *radius.Packet
	// LatencyUs is the monotonic microseconds between the final
	// transmission going on the wire and the matching reply being
	// received.
	LatencyUs int64
	// RetransmitN is the number of retransmits before success
	// (0 = original transmission succeeded).
	RetransmitN int
	// LocalAddr is the local UDP address the matching transmission
	// was sent from.
	LocalAddr net.Addr
	// Identifier is the RADIUS Identifier of the matching transmission.
	Identifier uint8
}

// Errors returned by the Engine.
var (
	// ErrIdentifierExhausted is returned by Send when no source tuple has
	// a free Identifier within the configured AllocMaxWait.
	ErrIdentifierExhausted = errors.New("io: identifier space exhausted across all source tuples")

	// ErrFinalTimeout is returned by Send after MaxRetries attempts all
	// timed out.
	ErrFinalTimeout = errors.New("io: final retransmit timeout")

	// ErrNoSockets is returned by NewEngine when SourceIPs is empty.
	ErrNoSockets = errors.New("io: no source IPs configured")

	// ErrNotStarted is returned by Send when called before Start.
	ErrNotStarted = errors.New("io: engine not started")

	// ErrAlreadyStarted is returned by Start when the engine is already running.
	ErrAlreadyStarted = errors.New("io: engine already started")
)

// Engine is the public type that owns the I/O lifecycle.
type Engine struct {
	opts     Opts
	pool     *SocketPool
	idAlloc  *IDAllocator
	matcher  *ReplyMatcher
	receiver *Receiver

	collector Collector

	startedAt    time.Time
	startedAtRaw atomic.Int64 // unix nanos; 0 = not started

	mu      sync.Mutex
	stopped bool
}

// NewEngine constructs an Engine. Sockets are bound immediately so the
// caller learns about port-bind failures up front. Start launches the
// receiver goroutines.
func NewEngine(opts Opts) (*Engine, error) {
	if len(opts.SourceIPs) == 0 {
		return nil, ErrNoSockets
	}
	if len(opts.Secret) == 0 {
		return nil, errors.New("io: shared secret required")
	}
	if opts.ReadBufBytes <= 0 {
		opts.ReadBufBytes = radius.MaxPacketLength
	}
	if opts.AllocBackoffStep <= 0 {
		opts.AllocBackoffStep = 5 * time.Millisecond
	}
	if opts.AllocMaxWait <= 0 {
		opts.AllocMaxWait = 100 * time.Millisecond
	}
	if opts.Collector == nil {
		opts.Collector = nopCollector{}
	}

	pool, err := NewSocketPool(opts.SourceIPs, opts.PortRangeLo, opts.PortRangeHi)
	if err != nil {
		return nil, fmt.Errorf("bind socket pool: %w", err)
	}

	idAlloc := NewIDAllocator()
	matcher := NewReplyMatcher(opts.Collector)

	e := &Engine{
		opts:      opts,
		pool:      pool,
		idAlloc:   idAlloc,
		matcher:   matcher,
		collector: opts.Collector,
	}
	e.receiver = NewReceiver(pool, matcher, opts.Handler, opts.Secret, opts.Collector, opts.ReadBufBytes)
	return e, nil
}

// Start launches the receiver. Idempotent error if already started.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return errors.New("io: engine has been stopped, create a new one")
	}
	if e.startedAtRaw.Load() != 0 {
		return ErrAlreadyStarted
	}
	now := time.Now()
	e.startedAt = now
	e.startedAtRaw.Store(now.UnixNano())
	e.receiver.Start(ctx)
	return nil
}

// Stop closes all sockets and waits for the receiver goroutines to exit.
// All outstanding Send calls observe this via context cancellation in the
// caller's ctx.
func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return nil
	}
	e.stopped = true

	e.receiver.Stop()
	e.matcher.CloseAll()
	e.pool.Close()
	return nil
}

// LocalAddrs returns the bound local UDP addresses (one per socket).
func (e *Engine) LocalAddrs() []*net.UDPAddr {
	return e.pool.LocalAddrs()
}

// Sender is the duck-typed I/O surface consumed by pkg/subscriber. The
// Engine satisfies it; tests substitute fakes. Defined on pkg/io so the
// canonical retransmit-policy / result types live with the implementation.
//
// build is a closure invoked once per transmission attempt with a fresh
// Identifier (per RFC 2865 §3 — fresh ID + authenticator on every
// retransmit). subID is forwarded into emitted Events purely for
// correlation; it is NOT serialised on the wire.
type Sender interface {
	Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error)
}

// Send is the public sender. dst is the destination RADIUS server. build
// is a closure invoked once per transmission attempt (each attempt gets a
// new Identifier per RFC). policy controls retransmit behaviour. subID is
// the subscriber id used in emitted Events. Returns a SendResult on
// success or an error on final timeout / cancellation / exhaustion.
func (e *Engine) Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error) {
	if e.startedAtRaw.Load() == 0 {
		return nil, ErrNotStarted
	}
	if policy.InitialTimeout <= 0 {
		policy.InitialTimeout = 5 * time.Second
	}
	return runSend(ctx, e, dst, build, policy, subID)
}

// offsetMs returns ms since Start for event timestamping.
func (e *Engine) offsetMs() int64 {
	t := e.startedAtRaw.Load()
	if t == 0 {
		return 0
	}
	return (time.Now().UnixNano() - t) / int64(time.Millisecond)
}

// resolveDst converts a net.Addr to a *net.UDPAddr (which sockets need).
func resolveDst(dst net.Addr) (*net.UDPAddr, error) {
	if u, ok := dst.(*net.UDPAddr); ok {
		return u, nil
	}
	if dst == nil {
		return nil, errors.New("io: nil destination address")
	}
	return net.ResolveUDPAddr("udp", dst.String())
}
