// Package subscriber — Subscriber struct + Run lifecycle.
//
// Purpose:
//
//	One Subscriber instance per simulated virtual subscriber. Run drives
//	the full Access-Request -> Access-Accept -> Accounting-Request ->
//	Accounting-Response flow through the FSM defined in state.go,
//	emitting Events to the collector at every transition and a final
//	SubscriberOutcome on terminal state.
//
//	Inbound CoA / Disconnect packets are delivered by the server
//	listener (slice 2D) via OnCoA / OnDisconnect; the FSM updates
//	state under the subscriber mutex without colliding with Run.
//
// Related files:
//   - pkg/subscriber/state.go      (State enum, terminal classifiers)
//   - pkg/io/io.go                 (Sender interface, RetransmitPolicy, SendResult — canonical types live here)
//   - pkg/subscriber/pool.go       (Pool that owns + indexes Subscribers)
//   - pkg/subscriber/lookup.go     (server-listener lookup contract)
//   - pkg/radius/constructors.go   (NewAccessRequestPAP/CHAP, NewAccountingRequestStart)
//   - .orchestration/contracts/event-schema.md (event shape)
//   - .orchestration/contracts/results-schema.md (outcome shape)
//
// Briefing: .orchestration/briefings/2b-subscriber.md
//
// Contract: Public — Subscriber and Deps are consumed by pkg/scenario
// (Wave 3) which constructs a Pool, drives activations on a schedule,
// and waits for outcomes to land in the collector.
package subscriber

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/io"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// Sender is the I/O surface the FSM consumes. Re-exported alias for
// io.Sender so existing call sites and test doubles can refer to a
// short name within this package while pkg/io owns the canonical
// definition.
type Sender = io.Sender

// RetransmitPolicy is re-exported from pkg/io for the same reason.
type RetransmitPolicy = io.RetransmitPolicy

// SendResult is re-exported from pkg/io.
type SendResult = io.SendResult

// PolicyFromConfig converts the user-facing config block into a
// RetransmitPolicy with defaults applied per docs/PROTOCOL.md
// "Retransmit policy".
func PolicyFromConfig(initialTimeoutMs, maxRetries int, backoff string, backoffBaseMs int) RetransmitPolicy {
	const (
		defaultInitialTimeout = 5 * time.Second
		defaultMaxRetries     = 3
		defaultBackoffBase    = 1 * time.Second
	)
	p := RetransmitPolicy{
		InitialTimeout: time.Duration(initialTimeoutMs) * time.Millisecond,
		MaxRetries:     maxRetries,
		BackoffBase:    time.Duration(backoffBaseMs) * time.Millisecond,
	}
	switch backoff {
	case "linear":
		p.Backoff = io.BackoffLinear
	case "constant", "fixed":
		p.Backoff = io.BackoffConstant
	default:
		p.Backoff = io.BackoffExponential
	}
	if p.InitialTimeout <= 0 {
		p.InitialTimeout = defaultInitialTimeout
	}
	if p.MaxRetries <= 0 {
		p.MaxRetries = defaultMaxRetries
	}
	if p.BackoffBase <= 0 {
		p.BackoffBase = defaultBackoffBase
	}
	return p
}

// Collector is the event sink the FSM emits to. Implemented by
// pkg/collector.Collector; declared here as an interface so tests can
// substitute an in-memory recorder.
type Collector interface {
	Submit(events.Event)
	SubmitOutcome(events.SubscriberOutcome)
}

// Deps bundles the runtime dependencies a Subscriber needs to execute
// its lifecycle. Constructed once per scenario by the scenario driver
// and passed to every Subscriber.Run call.
//
// All fields are required EXCEPT Now, which defaults to time.Now if
// nil — supplying it is mandatory in tests for determinism.
type Deps struct {
	// Sender is the outbound I/O surface (slice 2A pkg/io.Engine).
	Sender Sender

	// Collector is where events and the final outcome are emitted.
	Collector Collector

	// Config is the scenario config; we read NAS attrs, retransmit
	// policy, target addresses, IncludeAcctStart, and shared secret.
	Config *config.Config

	// AuthDst and AcctDst are the parsed target.auth_address and
	// target.acct_address. Resolution happens once per scenario, not
	// once per subscriber. If either is nil they will be parsed from
	// Config on first use.
	AuthDst net.Addr
	AcctDst net.Addr

	// Now is the clock function (defaults to time.Now). Tests inject
	// a fake clock to make event timestamps and outcome latencies
	// deterministic.
	Now func() time.Time

	// T0 is the test start time; used to compute the OffsetMs field
	// of every emitted event. If zero, Now() is used as a per-call
	// anchor (events still carry monotonic ns for latency math).
	T0 time.Time
}

// Subscriber is one virtual client. It owns a credential, a generated
// session identity (NAS-Port + Acct-Session-Id + Framed-IP-Address),
// the running FSM state, and a small bundle of timing + retransmit
// counters that feed the SubscriberOutcome.
//
// Concurrency model:
//
//   - Run is invoked on its own goroutine by the scenario driver
//     (typically once per subscriber). It is the only writer of
//     state-machine fields.
//
//   - OnCoA / OnDisconnect are invoked by the server listener
//     goroutine. They take the mutex briefly, snapshot/mutate state,
//     and emit events through the (already concurrency-safe) collector.
//
//   - Pool lookup readers (LookupByUsername etc.) hold a Pool-level
//     RWMutex around the index maps; they don't synchronise on the
//     Subscriber itself.
type Subscriber struct {
	// Identity ----------------------------------------------------
	id         uint32
	credential config.Credential
	authMethod string // "pap" | "chap"
	subType    string // "pppoe" | "mac"

	// Session identity (computed at construction so server-listener
	// lookups always see stable values).
	nasPort      uint32
	sessionID    string
	framedIP     net.IP
	callingStaID string
	nasPortID    string

	// FSM state ---------------------------------------------------
	mu              sync.RWMutex
	state           State
	activatedAt     time.Time
	establishedAt   time.Time
	disconnectedAt  time.Time
	authRetransmits int32
	acctRetransmits int32
	coaCount        atomic.Int32
	failureReason   string

	// Cached so OnDisconnect / OnCoA don't need a Deps reference.
	// Set by Run() the first time it executes; nil before Run starts.
	deps atomic.Pointer[Deps]
}

// New constructs a Subscriber with stable session identity derived from
// the supplied id. It does NOT start any goroutines or emit any events;
// emission happens at Run time so the test "T0" anchor is well-defined.
//
// The cred slice is captured by index (via id mod len) elsewhere — here
// we just take the credential we were handed.
func New(id uint32, cred config.Credential, cfg *config.Config) *Subscriber {
	s := &Subscriber{
		id:         id,
		credential: cred,
		state:      StateIdle,
	}
	s.authMethod = pickAuthMethod(cred, cfg)
	s.subType = pickSubType(cred, cfg)

	// Stable session identity.
	s.nasPort = id // 1:1 mapping is the simplest deterministic scheme
	s.sessionID = fmt.Sprintf("radstorm-%08x", id)

	// 10.<a>.<b>.<c> per id, gives us a 24-bit space (16M subs).
	s.framedIP = net.IPv4(10, byte(id>>16), byte(id>>8), byte(id)).To4()

	// Calling-Station-Id: prefer credential MAC, else synthesise one.
	if cred.MACAddress != "" {
		s.callingStaID = cred.MACAddress
	} else {
		// 02:rs:<id-bytes> — locally administered prefix.
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], id)
		s.callingStaID = fmt.Sprintf("02:72:73:%02x:%02x:%02x", b[1], b[2], b[3])
	}
	if cred.NASPortID != "" {
		s.nasPortID = cred.NASPortID
	} else {
		s.nasPortID = fmt.Sprintf("0/0/%d", id)
	}

	return s
}

// ID returns the immutable subscriber id assigned by the pool.
func (s *Subscriber) ID() uint32 { return s.id }

// Username returns the credential username.
func (s *Subscriber) Username() string { return s.credential.Username }

// SessionID returns the synthesised Acct-Session-Id (stable for the
// life of this Subscriber).
func (s *Subscriber) SessionID() string { return s.sessionID }

// FramedIP returns the synthesised Framed-IP-Address.
func (s *Subscriber) FramedIP() net.IP { return s.framedIP }

// AuthMethod returns "pap" or "chap".
func (s *Subscriber) AuthMethod() string { return s.authMethod }

// SubType returns "pppoe" or "mac".
func (s *Subscriber) SubType() string { return s.subType }

// State returns the current FSM state in a thread-safe way.
func (s *Subscriber) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// CoaCount returns the number of CoA-Requests received for this
// subscriber. Atomic (server listener increments concurrently with Run).
func (s *Subscriber) CoaCount() int32 { return s.coaCount.Load() }

// pickAuthMethod respects the credential's auth_method column if set,
// otherwise consults config.Subscribers.AuthMethodPapPct to assign
// based on subscriber id (deterministic).
func pickAuthMethod(cred config.Credential, cfg *config.Config) string {
	if cred.AuthMethod != "" {
		return cred.AuthMethod
	}
	if cfg == nil || cfg.Subscribers.AuthMethodPapPct >= 100 {
		return events.AuthMethodPAP
	}
	if cfg.Subscribers.AuthMethodPapPct <= 0 {
		return events.AuthMethodCHAP
	}
	return events.AuthMethodPAP
}

func pickSubType(cred config.Credential, cfg *config.Config) string {
	if cred.SubType != "" {
		return cred.SubType
	}
	if cfg == nil || cfg.Subscribers.TypePppoePct >= 100 {
		return events.SubTypePPPoE
	}
	if cfg.Subscribers.TypePppoePct <= 0 {
		return events.SubTypeMAC
	}
	return events.SubTypePPPoE
}

// Run executes the full FSM for this subscriber. Returns the terminal
// State (one of auth_failed / acct_failed / established / terminated)
// and any unexpected error. A normal "the server rejected us" outcome
// returns nil error — the FSM treats reject as a successful test
// observation.
//
// Lifecycle:
//
//  1. activated -> auth_sent: build Access-Request, call Sender.Send
//  2. on Access-Accept:
//     - if IncludeAcctStart: acct_sent -> Accounting-Request
//     - else: established
//  3. on Access-Reject: auth_failed
//  4. on send/timeout error: auth_failed (auth phase) or acct_failed (acct phase)
//
// Emits a SubscriberOutcome on every terminal exit.
func (s *Subscriber) Run(ctx context.Context, deps Deps) (State, error) {
	if deps.Sender == nil {
		return StateIdle, errors.New("subscriber: Deps.Sender is nil")
	}
	if deps.Collector == nil {
		return StateIdle, errors.New("subscriber: Deps.Collector is nil")
	}
	if deps.Config == nil {
		return StateIdle, errors.New("subscriber: Deps.Config is nil")
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}

	// Resolve target addresses if the scenario driver didn't pre-resolve.
	if deps.AuthDst == nil {
		addr, err := net.ResolveUDPAddr("udp", deps.Config.Target.AuthAddress)
		if err != nil {
			return StateIdle, fmt.Errorf("resolve auth address: %w", err)
		}
		deps.AuthDst = addr
	}
	if deps.AcctDst == nil && deps.Config.Subscribers.IncludeAcctStart {
		addr, err := net.ResolveUDPAddr("udp", deps.Config.Target.AcctAddress)
		if err != nil {
			return StateIdle, fmt.Errorf("resolve acct address: %w", err)
		}
		deps.AcctDst = addr
	}

	// Stash deps so OnCoA/OnDisconnect can emit events without the
	// scenario driver passing them in.
	s.deps.Store(&deps)

	now := deps.Now()
	s.mu.Lock()
	s.activatedAt = now
	s.mu.Unlock()

	// Emit "activated" event.
	deps.Collector.Submit(events.NewSubscriberActivated(s.id, s.offsetMs(deps, now), string(StateIdle)))

	policy := PolicyFromConfig(
		deps.Config.Retransmit.InitialTimeoutMs,
		deps.Config.Retransmit.MaxRetries,
		deps.Config.Retransmit.Backoff,
		deps.Config.Retransmit.BackoffBaseMs,
	)

	// Phase 1: Access-Request.
	s.transition(deps, StateAuthSent, "")

	authRes, err := deps.Sender.Send(ctx, deps.AuthDst, s.buildAccessRequest(deps), policy, s.id)
	s.recordAuthRetransmits(authRes)

	if err != nil || authRes == nil {
		reason := "auth send error"
		if err != nil {
			reason = err.Error()
		}
		// On a terminal Send error pkg/io discards the partial SendResult,
		// so the retransmit count is unavailable. If the error looks like
		// a timeout-after-retries, attribute the configured MaxRetries so
		// the per-subscriber outcome carries the full attempt count.
		if authRes == nil && err != nil && isFinalTimeoutErr(err) {
			atomic.StoreInt32(&s.authRetransmits, int32(policy.MaxRetries))
		}
		return s.terminate(deps, StateAuthFailed, reason), nil
	}

	if authRes.Reply == nil {
		return s.terminate(deps, StateAuthFailed, "auth: nil reply"), nil
	}

	s.emitReplyEvent(deps, authRes, deps.AuthDst, string(StateAuthSent))

	switch authRes.Reply.Code {
	case radius.CodeAccessAccept:
		// Continue to acct or short-circuit to established.
	case radius.CodeAccessReject:
		return s.terminate(deps, StateAuthFailed, "Access-Reject"), nil
	default:
		return s.terminate(deps, StateAuthFailed,
			fmt.Sprintf("unexpected auth reply code %d", authRes.Reply.Code)), nil
	}

	if !deps.Config.Subscribers.IncludeAcctStart {
		return s.terminate(deps, StateEstablished, ""), nil
	}

	// Phase 2: Accounting-Request (Start).
	s.transition(deps, StateAcctSent, "")

	acctRes, err := deps.Sender.Send(ctx, deps.AcctDst, s.buildAccountingStart(deps), policy, s.id)
	s.recordAcctRetransmits(acctRes)

	if err != nil || acctRes == nil {
		reason := "acct send error"
		if err != nil {
			reason = err.Error()
		}
		if acctRes == nil && err != nil && isFinalTimeoutErr(err) {
			atomic.StoreInt32(&s.acctRetransmits, int32(policy.MaxRetries))
		}
		return s.terminate(deps, StateAcctFailed, reason), nil
	}

	if acctRes.Reply == nil {
		return s.terminate(deps, StateAcctFailed, "acct: nil reply"), nil
	}

	s.emitReplyEvent(deps, acctRes, deps.AcctDst, string(StateAcctSent))

	if acctRes.Reply.Code != radius.CodeAccountingResponse {
		return s.terminate(deps, StateAcctFailed,
			fmt.Sprintf("unexpected acct reply code %d", acctRes.Reply.Code)), nil
	}

	return s.terminate(deps, StateEstablished, ""), nil
}

// buildAccessRequest returns a build-closure suitable for Sender.Send.
// The closure is called once per attempt with a fresh Identifier; each
// invocation therefore produces a fresh request authenticator (random
// for PAP, fresh CHAP-Identifier+Challenge for CHAP) per RFC 2865 §3.
func (s *Subscriber) buildAccessRequest(deps Deps) func(id uint8) (*radius.Packet, error) {
	secret := []byte(deps.Config.Target.SharedSecret)
	username := s.credential.Username
	password := s.credential.Password

	// Pre-allocate the standard NAS / framing extras shared across PAP
	// and CHAP. We rebuild per-attempt because an extra slice mutated
	// across calls could race with retries running on the I/O worker.
	makeExtras := func() []radius.Attribute {
		var extras []radius.Attribute
		// NAS-IP-Address
		if ip := net.ParseIP(deps.Config.NAS.IPAddress).To4(); ip != nil {
			extras = append(extras, radius.Attribute{Type: radius.AttrNASIPAddress, Value: ip})
		}
		// NAS-Port (uint32)
		var np [4]byte
		binary.BigEndian.PutUint32(np[:], s.nasPort)
		extras = append(extras, radius.Attribute{Type: radius.AttrNASPort, Value: np[:]})

		// Service-Type, Framed-Protocol — only for PPPoE-style.
		if s.subType == events.SubTypePPPoE {
			var st [4]byte
			binary.BigEndian.PutUint32(st[:], radius.ServiceTypeFramed)
			extras = append(extras, radius.Attribute{Type: radius.AttrServiceType, Value: st[:]})
			var fp [4]byte
			binary.BigEndian.PutUint32(fp[:], radius.FramedProtocolPPP)
			extras = append(extras, radius.Attribute{Type: radius.AttrFramedProtocol, Value: fp[:]})
		}

		// Calling-Station-Id (subscriber MAC) and NAS-Identifier.
		extras = append(extras, radius.Attribute{Type: radius.AttrCallingStationID, Value: []byte(s.callingStaID)})
		extras = append(extras, radius.Attribute{Type: radius.AttrNASIdentifier, Value: []byte(deps.Config.NAS.Identifier)})

		// NAS-Port-Type
		var nt [4]byte
		switch s.subType {
		case events.SubTypeMAC:
			binary.BigEndian.PutUint32(nt[:], radius.NASPortTypeEthernet)
		default:
			binary.BigEndian.PutUint32(nt[:], radius.NASPortTypeVirtual)
		}
		extras = append(extras, radius.Attribute{Type: radius.AttrNASPortType, Value: nt[:]})

		// NAS-Port-Id (string)
		extras = append(extras, radius.Attribute{Type: radius.AttrNASPortID, Value: []byte(s.nasPortID)})
		return extras
	}

	if s.authMethod == events.AuthMethodCHAP {
		return func(id uint8) (*radius.Packet, error) {
			return radius.NewAccessRequestCHAP(id, secret, username, password, makeExtras()...)
		}
	}
	return func(id uint8) (*radius.Packet, error) {
		return radius.NewAccessRequestPAP(id, secret, username, password, makeExtras()...)
	}
}

// buildAccountingStart returns a build-closure for the Acct-Status-Type
// = Start request. Acct-Session-Id and Framed-IP-Address are derived
// from the subscriber identity.
func (s *Subscriber) buildAccountingStart(deps Deps) func(id uint8) (*radius.Packet, error) {
	secret := []byte(deps.Config.Target.SharedSecret)

	makeExtras := func() []radius.Attribute {
		var extras []radius.Attribute
		extras = append(extras, radius.Attribute{Type: radius.AttrUserName, Value: []byte(s.credential.Username)})

		if ip := net.ParseIP(deps.Config.NAS.IPAddress).To4(); ip != nil {
			extras = append(extras, radius.Attribute{Type: radius.AttrNASIPAddress, Value: ip})
		}
		var np [4]byte
		binary.BigEndian.PutUint32(np[:], s.nasPort)
		extras = append(extras, radius.Attribute{Type: radius.AttrNASPort, Value: np[:]})

		// Framed-IP-Address (the subscriber's mock-assigned IP).
		extras = append(extras, radius.Attribute{Type: radius.AttrFramedIPAddress, Value: s.framedIP})

		extras = append(extras, radius.Attribute{Type: radius.AttrCallingStationID, Value: []byte(s.callingStaID)})
		extras = append(extras, radius.Attribute{Type: radius.AttrNASIdentifier, Value: []byte(deps.Config.NAS.Identifier)})

		// Acct-Session-Id and Acct-Authentic.
		extras = append(extras, radius.Attribute{Type: radius.AttrAcctSessionID, Value: []byte(s.sessionID)})
		var aa [4]byte
		binary.BigEndian.PutUint32(aa[:], radius.AcctAuthenticRADIUS)
		extras = append(extras, radius.Attribute{Type: radius.AttrAcctAuthentic, Value: aa[:]})

		var nt [4]byte
		switch s.subType {
		case events.SubTypeMAC:
			binary.BigEndian.PutUint32(nt[:], radius.NASPortTypeEthernet)
		default:
			binary.BigEndian.PutUint32(nt[:], radius.NASPortTypeVirtual)
		}
		extras = append(extras, radius.Attribute{Type: radius.AttrNASPortType, Value: nt[:]})
		extras = append(extras, radius.Attribute{Type: radius.AttrNASPortID, Value: []byte(s.nasPortID)})
		return extras
	}

	return func(id uint8) (*radius.Packet, error) {
		return radius.NewAccountingRequestStart(id, secret, makeExtras()...)
	}
}

// transition updates the FSM state and emits a state_changed event.
// reason is currently unused but reserved for failure transitions
// where we want a free-form note attached.
func (s *Subscriber) transition(deps Deps, next State, reason string) {
	s.mu.Lock()
	prev := s.state
	if prev == next {
		s.mu.Unlock()
		return
	}
	s.state = next
	s.mu.Unlock()

	now := deps.Now()
	ev := events.NewStateChanged(s.id, s.offsetMs(deps, now), string(next))
	if reason != "" {
		ev.ErrorMessage = reason
	}
	deps.Collector.Submit(ev)
}

// terminate is the single exit point from Run. It performs the final
// state transition, emits a terminal event, and submits the
// per-subscriber outcome record. Returns the terminal state.
func (s *Subscriber) terminate(deps Deps, terminal State, failureReason string) State {
	s.mu.Lock()
	prevState := s.state
	s.state = terminal
	if terminal == StateEstablished {
		s.establishedAt = deps.Now()
	}
	if failureReason != "" {
		s.failureReason = failureReason
	}
	s.mu.Unlock()

	if prevState != terminal {
		// Emit state_changed *before* the terminal event so the timeline
		// reads naturally even when the previous state was an in-flight
		// state.
		deps.Collector.Submit(events.NewStateChanged(s.id, s.offsetMs(deps, deps.Now()), string(terminal)))
	}

	deps.Collector.Submit(events.NewTerminal(s.id, s.offsetMs(deps, deps.Now()), string(terminal)))
	deps.Collector.SubmitOutcome(s.buildOutcome(deps, terminal, failureReason))
	return terminal
}

// buildOutcome serialises the current Subscriber into a SubscriberOutcome.
// Latency / offset milliseconds are always recorded as ms-since-T0, with
// -1 sentinels for milestones that didn't occur (per results-schema.md).
func (s *Subscriber) buildOutcome(deps Deps, terminal State, failureReason string) events.SubscriberOutcome {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := events.SubscriberOutcome{
		SubscriberID:           s.id,
		Username:               s.credential.Username,
		AuthMethod:             s.authMethod,
		SubType:                s.subType,
		FinalState:             string(terminal),
		ActivatedAtOffsetMs:    s.toOffsetMs(deps, s.activatedAt),
		EstablishedAtOffsetMs:  events.NotApplicableMs,
		EstablishmentLatencyMs: events.NotApplicableMs,
		AuthRetransmits:        atomic.LoadInt32(&s.authRetransmits),
		AcctRetransmits:        atomic.LoadInt32(&s.acctRetransmits),
		CoaReceivedCount:       s.coaCount.Load(),
		DisconnectReceivedAt:   events.NotApplicableMs,
		FailureReason:          failureReason,
	}
	if !s.establishedAt.IsZero() {
		out.EstablishedAtOffsetMs = s.toOffsetMs(deps, s.establishedAt)
		if !s.activatedAt.IsZero() {
			out.EstablishmentLatencyMs = s.establishedAt.Sub(s.activatedAt).Milliseconds()
		}
	}
	if !s.disconnectedAt.IsZero() {
		out.DisconnectReceivedAt = s.toOffsetMs(deps, s.disconnectedAt)
	}
	if terminal == StateEstablished && failureReason == "" {
		out.FailureReason = ""
	}
	return out
}

// recordAuthRetransmits stores the retransmit count from an auth send.
func (s *Subscriber) recordAuthRetransmits(res *SendResult) {
	if res == nil {
		return
	}
	atomic.StoreInt32(&s.authRetransmits, int32(res.RetransmitN))
}

// recordAcctRetransmits stores the retransmit count from an accounting send.
func (s *Subscriber) recordAcctRetransmits(res *SendResult) {
	if res == nil {
		return
	}
	atomic.StoreInt32(&s.acctRetransmits, int32(res.RetransmitN))
}

// emitReplyEvent records a matched reply in the Parquet event log.
//
// pkg/io.SendResult does NOT carry the remote address or wire bytes
// (the I/O layer's own receiver already emitted reply_received with
// those fields). The FSM emits its own reply_received tagged with the
// CURRENT state — the duplicate is intentional: callers reading just
// the subscriber-correlated stream see the per-state context, while
// the I/O-layer's emission carries the wire-level facts.
func (s *Subscriber) emitReplyEvent(deps Deps, res *SendResult, dst net.Addr, currentState string) {
	if res == nil || res.Reply == nil {
		return
	}
	now := deps.Now()
	// Compute reply size by re-encoding (cheap; the I/O layer has
	// already validated the authenticator). 0 on encode error.
	var bytes int32
	if wire, err := res.Reply.Encode(); err == nil {
		bytes = int32(len(wire))
	}
	ev := events.NewReplyReceived(
		s.id,
		s.offsetMs(deps, now),
		currentState,
		int8(res.Reply.Code),
		int16(res.Reply.Identifier),
		addrString(res.LocalAddr),
		addrString(dst),
		bytes,
		res.LatencyUs,
	)
	deps.Collector.Submit(ev)
}

// offsetMs converts the Now reading to milliseconds since T0. If T0
// is unset, returns 0 (the stream is still ordered by MonotonicNs).
func (s *Subscriber) offsetMs(deps Deps, now time.Time) int64 {
	if deps.T0.IsZero() {
		return 0
	}
	return now.Sub(deps.T0).Milliseconds()
}

func (s *Subscriber) toOffsetMs(deps Deps, t time.Time) int64 {
	if t.IsZero() || deps.T0.IsZero() {
		return events.NotApplicableMs
	}
	return t.Sub(deps.T0).Milliseconds()
}

// OnCoA is called by the server listener when a CoA-Request targets
// this subscriber. We don't currently re-evaluate session state from
// VSAs (that's a future work item per docs/PROTOCOL.md); we just bump
// the counter and emit an event.
//
// Safe to call from any goroutine. Does not transition the FSM.
func (s *Subscriber) OnCoA(p *radius.Packet) {
	s.coaCount.Add(1)
	deps := s.deps.Load()
	if deps == nil || deps.Collector == nil {
		return
	}
	now := deps.Now()
	id := int16(events.NoIdentifier)
	if p != nil {
		id = int16(p.Identifier)
	}
	deps.Collector.Submit(events.NewCoAEvent(
		s.id, s.offsetMs(*deps, now), events.EventTypeCoAReceived,
		id, "", "", 0, 0, 0,
	))
}

// OnDisconnect transitions the subscriber to terminated if it was
// established, emitting a state_changed event and an outcome update.
//
// Safe to call from any goroutine. The current state is consulted
// under the subscriber mutex so a race between Run completing and a
// disconnect arriving does not double-emit the outcome.
func (s *Subscriber) OnDisconnect(p *radius.Packet) {
	deps := s.deps.Load()
	if deps == nil || deps.Collector == nil {
		return
	}

	s.mu.Lock()
	prev := s.state
	if prev == StateTerminated {
		s.mu.Unlock()
		return
	}
	s.state = StateTerminated
	s.disconnectedAt = deps.Now()
	s.mu.Unlock()

	now := deps.Now()
	id := int16(events.NoIdentifier)
	if p != nil {
		id = int16(p.Identifier)
	}
	deps.Collector.Submit(events.NewDisconnectEvent(
		s.id, s.offsetMs(*deps, now), events.EventTypeDisconnectReceived,
		id, "", "", 0, 0, 0,
	))
	deps.Collector.Submit(events.NewStateChanged(s.id, s.offsetMs(*deps, now), string(StateTerminated)))

	// Emit a fresh outcome reflecting the terminated state. The
	// collector's outcome writer accepts the new row; downstream
	// readers de-dup on (sub_id, final_state) when they care.
	deps.Collector.SubmitOutcome(s.buildOutcome(*deps, StateTerminated, "Disconnect-Request received"))
}

// addrString safely renders a net.Addr (handles nil).
func addrString(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

// isFinalTimeoutErr reports whether err looks like the I/O layer's
// retransmit-exhausted timeout. We match on the canonical io.ErrFinalTimeout
// and also any error whose message contains "timeout" so wrapped errors
// (and the test fake's plain errors.New("timeout exhausted")) attribute
// the configured MaxRetries to the per-subscriber outcome.
func isFinalTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrFinalTimeout) {
		return true
	}
	msg := err.Error()
	for i := 0; i+7 <= len(msg); i++ {
		if msg[i] == 't' && msg[i+1] == 'i' && msg[i+2] == 'm' && msg[i+3] == 'e' && msg[i+4] == 'o' && msg[i+5] == 'u' && msg[i+6] == 't' {
			return true
		}
	}
	return false
}
