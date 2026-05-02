// Package subscriber — FSM lifecycle tests.
//
// Purpose:
//
//	Drives the Subscriber.Run state machine against a scripted fake
//	Sender + recording fakeCollector to assert the full happy paths
//	(PAP + CHAP, with and without Acct-Start) and every failure path
//	(Access-Reject, auth timeout, accounting timeout, accounting
//	NAK-style unexpected reply).
//
// Related files:
//   - pkg/subscriber/subscriber.go (system under test)
//   - pkg/subscriber/state.go      (terminal classifiers asserted here)
//   - pkg/subscriber/fakes_test.go (test doubles)
//
// Briefing: .orchestration/briefings/2b-subscriber.md
package subscriber

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/radstorm/pkg/config"
	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/io"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// fixedClock returns the same instant on every call. Useful for
// deterministic offset / latency assertions.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// stepClock returns successive instants, advancing by step on each call.
// The first call returns base; the second base+step; etc.
func stepClock(base time.Time, step time.Duration) func() time.Time {
	now := base
	return func() time.Time {
		ret := now
		now = now.Add(step)
		return ret
	}
}

// baseConfig returns a minimal but valid Config for FSM tests.
func baseConfig(includeAcct bool) *config.Config {
	return &config.Config{
		Target: config.Target{
			AuthAddress:  "127.0.0.1:11812",
			AcctAddress:  "127.0.0.1:11813",
			SharedSecret: "testing123",
		},
		Subscribers: config.Subscribers{
			Count:            1,
			AuthMethodPapPct: 100,
			TypePppoePct:     100,
			IncludeAcctStart: includeAcct,
		},
		NAS: config.NAS{
			IPAddress:  "10.0.0.1",
			Identifier: "radstorm-test",
		},
		Retransmit: config.Retransmit{
			InitialTimeoutMs: 100,
			MaxRetries:       2,
			Backoff:          "exponential",
			BackoffBaseMs:    10,
		},
		Scenario: config.Scenario{Type: "uniform"},
	}
}

func TestSubscriber_HappyPath_PAP_AuthOnly(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "alice", Password: "secret", AuthMethod: events.AuthMethodPAP}
	sub := New(1, cred, cfg)

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccessAccept, latency: 12 * time.Millisecond})
	col := newFakeCollector()

	t0 := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	deps := Deps{
		Sender:    sender,
		Collector: col,
		Config:    cfg,
		Now:       stepClock(t0, time.Millisecond),
		T0:        t0,
	}
	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateEstablished, final)

	require.Equal(t, 1, sender.callCount(), "PAP-only should send exactly one packet")
	call := sender.recordedCall(0)
	require.Equal(t, radius.CodeAccessRequest, call.built.Code)
	// User-Password attribute (PAP) must be present, CHAP-Password absent.
	_, hasUP := call.built.Attributes.Get(radius.AttrUserPassword)
	_, hasCP := call.built.Attributes.Get(radius.AttrCHAPPassword)
	assert.True(t, hasUP, "PAP request must carry User-Password")
	assert.False(t, hasCP, "PAP request must NOT carry CHAP-Password")

	// Outcome assertions.
	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	out := outs[0]
	assert.Equal(t, "alice", out.Username)
	assert.Equal(t, events.AuthMethodPAP, out.AuthMethod)
	assert.Equal(t, events.FinalStateEstablished, out.FinalState)
	assert.Equal(t, int32(0), out.AuthRetransmits)
	assert.Equal(t, "", out.FailureReason)

	// Event log: must include activated, state_changed -> auth_sent,
	// reply_received, state_changed -> established, terminal.
	assert.True(t, col.hasEventOfType(events.CategorySubscriberLifecycle, events.EventTypeSubscriberActivated))
	assert.True(t, col.stateChangedTo(string(StateAuthSent)))
	assert.True(t, col.stateChangedTo(string(StateEstablished)))
	assert.True(t, col.hasEventOfType(events.CategoryPacketInbound, events.EventTypeReplyReceived))
	assert.True(t, col.hasEventOfType(events.CategorySubscriberLifecycle, events.EventTypeSubscriberTerminal))
}

func TestSubscriber_HappyPath_PAP_WithAcctStart(t *testing.T) {
	cfg := baseConfig(true)
	cred := config.Credential{Username: "bob", Password: "hunter2", AuthMethod: events.AuthMethodPAP}
	sub := New(2, cred, cfg)

	sender := newFakeSender(
		scriptedReply{replyCode: radius.CodeAccessAccept, latency: 5 * time.Millisecond},
		scriptedReply{replyCode: radius.CodeAccountingResponse, latency: 7 * time.Millisecond},
	)
	col := newFakeCollector()

	t0 := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: stepClock(t0, time.Millisecond), T0: t0}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateEstablished, final)

	require.Equal(t, 2, sender.callCount())
	// First call -> Access-Request, second -> Accounting-Request.
	assert.Equal(t, radius.CodeAccessRequest, sender.recordedCall(0).built.Code)
	assert.Equal(t, radius.CodeAccountingRequest, sender.recordedCall(1).built.Code)

	// Acct request must carry Acct-Status-Type + Acct-Session-Id +
	// Framed-IP-Address.
	acct := sender.recordedCall(1).built
	statusType, ok := acct.Attributes.GetUint32(radius.AttrAcctStatusType)
	require.True(t, ok)
	assert.Equal(t, radius.AcctStatusStart, statusType)
	assert.NotEmpty(t, acct.Attributes.GetString(radius.AttrAcctSessionID))
	_, ok = acct.Attributes.GetIPv4(radius.AttrFramedIPAddress)
	assert.True(t, ok)

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Equal(t, events.FinalStateEstablished, outs[0].FinalState)
}

func TestSubscriber_HappyPath_CHAP(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.AuthMethodPapPct = 0
	cred := config.Credential{Username: "carol", Password: "letmein", AuthMethod: events.AuthMethodCHAP}
	sub := New(3, cred, cfg)
	require.Equal(t, events.AuthMethodCHAP, sub.AuthMethod())

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccessAccept})
	col := newFakeCollector()

	deps := Deps{
		Sender: sender, Collector: col, Config: cfg,
		Now: time.Now, T0: time.Now(),
	}
	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateEstablished, final)

	built := sender.recordedCall(0).built
	_, hasCP := built.Attributes.Get(radius.AttrCHAPPassword)
	_, hasCC := built.Attributes.Get(radius.AttrCHAPChallenge)
	_, hasUP := built.Attributes.Get(radius.AttrUserPassword)
	assert.True(t, hasCP, "CHAP request must carry CHAP-Password")
	assert.True(t, hasCC, "CHAP request must carry CHAP-Challenge")
	assert.False(t, hasUP, "CHAP request must NOT carry User-Password")

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Equal(t, events.AuthMethodCHAP, outs[0].AuthMethod)
}

func TestSubscriber_AuthRejected(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "evil", Password: "wrong"}
	sub := New(4, cred, cfg)

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccessReject})
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateAuthFailed, final)

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Equal(t, events.FinalStateAuthFailed, outs[0].FinalState)
	assert.Contains(t, outs[0].FailureReason, "Access-Reject")
	// Establishment fields stay at the not-applicable sentinel.
	assert.Equal(t, events.NotApplicableMs, outs[0].EstablishedAtOffsetMs)
	assert.Equal(t, events.NotApplicableMs, outs[0].EstablishmentLatencyMs)
}

func TestSubscriber_AuthTimeout_RetransmitsRecorded(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "stuck", Password: "x"}
	sub := New(5, cred, cfg)

	// io.Engine.Send returns (nil, ErrFinalTimeout) on retransmit
	// exhaustion — the partial result is discarded. Subscriber attributes
	// the configured MaxRetries to the outcome in that case.
	sender := newFakeSender(scriptedReply{
		err: errors.New("timeout exhausted"),
	})
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateAuthFailed, final)

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Equal(t, events.FinalStateAuthFailed, outs[0].FinalState)
	assert.Equal(t, int32(cfg.Retransmit.MaxRetries), outs[0].AuthRetransmits,
		"on terminal timeout the outcome attributes MaxRetries to the subscriber")
	assert.Contains(t, outs[0].FailureReason, "timeout")
}

func TestSubscriber_AuthSendHardError(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "broken", Password: "x"}
	sub := New(6, cred, cfg)

	sender := newFakeSender(scriptedReply{wantHardErr: true, err: errors.New("socket closed")})
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateAuthFailed, final)

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Contains(t, outs[0].FailureReason, "socket")
}

func TestSubscriber_AcctTimeout(t *testing.T) {
	cfg := baseConfig(true)
	cred := config.Credential{Username: "halfway", Password: "x"}
	sub := New(7, cred, cfg)

	sender := newFakeSender(
		scriptedReply{replyCode: radius.CodeAccessAccept},
		scriptedReply{err: errors.New("acct timeout"), retransmits: 2},
	)
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateAcctFailed, final)

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Equal(t, events.FinalStateAcctFailed, outs[0].FinalState)
	assert.Equal(t, int32(2), outs[0].AcctRetransmits)
	assert.Equal(t, int32(0), outs[0].AuthRetransmits)
	assert.Contains(t, outs[0].FailureReason, "timeout")
}

func TestSubscriber_AcctUnexpectedReplyCode(t *testing.T) {
	cfg := baseConfig(true)
	cred := config.Credential{Username: "weird", Password: "x"}
	sub := New(8, cred, cfg)

	sender := newFakeSender(
		scriptedReply{replyCode: radius.CodeAccessAccept},
		scriptedReply{replyCode: radius.CodeAccessReject}, // wrong reply for acct phase
	)
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateAcctFailed, final)
}

func TestSubscriber_AuthUnexpectedReplyCode(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "weird", Password: "x"}
	sub := New(9, cred, cfg)

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccountingResponse})
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateAuthFailed, final)

	outs := col.snapshotOutcomes()
	require.Len(t, outs, 1)
	assert.Contains(t, outs[0].FailureReason, "unexpected auth reply")
}

func TestSubscriber_OnDisconnect_TransitionsTerminated(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "kicked", Password: "x"}
	sub := New(10, cred, cfg)

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccessAccept})
	col := newFakeCollector()
	t0 := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: stepClock(t0, time.Millisecond), T0: t0}

	final, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateEstablished, final)
	require.Equal(t, StateEstablished, sub.State())

	// Now arrive a Disconnect-Request out of band.
	disc := &radius.Packet{Code: radius.CodeDisconnectRequest, Identifier: 99}
	sub.OnDisconnect(disc)

	require.Equal(t, StateTerminated, sub.State())
	assert.True(t, col.hasEventOfType(events.CategoryDisconnectInbound, events.EventTypeDisconnectReceived))
	assert.True(t, col.stateChangedTo(string(StateTerminated)))

	// A second outcome row should have been submitted reflecting the
	// terminated state.
	outs := col.snapshotOutcomes()
	require.GreaterOrEqual(t, len(outs), 2)
	last := outs[len(outs)-1]
	assert.Equal(t, events.FinalStateTerminated, last.FinalState)
	assert.Contains(t, last.FailureReason, "Disconnect")

	// Calling OnDisconnect again is a no-op (idempotent).
	beforeCount := len(col.snapshotOutcomes())
	sub.OnDisconnect(disc)
	assert.Equal(t, beforeCount, len(col.snapshotOutcomes()))
}

func TestSubscriber_OnCoA_LeavesStateUnchanged(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "coa-target", Password: "x"}
	sub := New(11, cred, cfg)

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccessAccept})
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}
	_, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, StateEstablished, sub.State())

	coa := &radius.Packet{Code: radius.CodeCoARequest, Identifier: 17}
	sub.OnCoA(coa)
	sub.OnCoA(coa)

	assert.Equal(t, StateEstablished, sub.State(), "CoA must NOT change FSM state")
	assert.Equal(t, int32(2), sub.CoaCount())
	assert.True(t, col.hasEventOfType(events.CategoryCoAInbound, events.EventTypeCoAReceived))
}

func TestSubscriber_Run_RejectsNilDeps(t *testing.T) {
	cfg := baseConfig(false)
	sub := New(12, config.Credential{Username: "x", Password: "y"}, cfg)

	cases := []struct {
		name string
		deps Deps
	}{
		{"missing sender", Deps{Collector: newFakeCollector(), Config: cfg}},
		{"missing collector", Deps{Sender: newFakeSender(), Config: cfg}},
		{"missing config", Deps{Sender: newFakeSender(), Collector: newFakeCollector()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sub.Run(context.Background(), tc.deps)
			require.Error(t, err)
		})
	}
}

func TestSubscriber_Identity(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "iden", Password: "p", MACAddress: "aa:bb:cc:dd:ee:ff", NASPortID: "1/2/3"}
	sub := New(0xCAFEBABE, cred, cfg)

	assert.Equal(t, uint32(0xCAFEBABE), sub.ID())
	assert.Equal(t, "iden", sub.Username())
	assert.Equal(t, "radstorm-cafebabe", sub.SessionID())
	assert.NotNil(t, sub.FramedIP())
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", sub.callingStaID)
	assert.Equal(t, "1/2/3", sub.nasPortID)

	// Without MAC, a synthesised one is generated.
	sub2 := New(7, config.Credential{Username: "u", Password: "p"}, cfg)
	assert.Regexp(t, `^02:72:73:[0-9a-f]{2}:[0-9a-f]{2}:07$`, sub2.callingStaID)
	assert.Equal(t, "0/0/7", sub2.nasPortID)
}

func TestState_Classifiers(t *testing.T) {
	for _, s := range []State{StateAuthFailed, StateAcctFailed, StateEstablished, StateTerminated} {
		assert.True(t, s.IsTerminal(), "%s must be terminal", s)
	}
	for _, s := range []State{StateIdle, StateAuthSent, StateAuthRetry, StateAcctSent, StateAcctRetry} {
		assert.False(t, s.IsTerminal(), "%s must NOT be terminal", s)
	}
	assert.True(t, StateAuthFailed.IsFailure())
	assert.True(t, StateAcctFailed.IsFailure())
	assert.False(t, StateEstablished.IsFailure())
}

func TestPolicyFromConfig_Defaults(t *testing.T) {
	p := PolicyFromConfig(0, 0, "", 0)
	assert.Equal(t, 5*time.Second, p.InitialTimeout)
	assert.Equal(t, 3, p.MaxRetries)
	assert.Equal(t, io.BackoffExponential, p.Backoff)
	assert.Equal(t, 1*time.Second, p.BackoffBase)

	p = PolicyFromConfig(2500, 7, "fixed", 250)
	assert.Equal(t, 2500*time.Millisecond, p.InitialTimeout)
	assert.Equal(t, 7, p.MaxRetries)
	assert.Equal(t, io.BackoffConstant, p.Backoff)
	assert.Equal(t, 250*time.Millisecond, p.BackoffBase)
}

func TestSendResult_LatencyUs(t *testing.T) {
	// io.SendResult exposes LatencyUs (microseconds) instead of a Latency()
	// helper. Round-trip a known value to confirm the field is preserved.
	r := &SendResult{LatencyUs: 123}
	assert.Equal(t, int64(123), r.LatencyUs)
}

// fixedClock is referenced via a top-level _ to silence the linter
// when tests don't all use it. Compile-time noop.
var _ = fixedClock
