// Package io — ReplyMatcher tests.
//
// Purpose:
//   Verifies the matcher: register/deliver round-trip, duplicate
//   detection, bad-authenticator rejection, unmatched delivery, and
//   cancellation.
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

func mkUDPAddr(ip string, port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: port}
}

// makeReplyForRequest builds a wire-encoded Access-Accept whose
// authenticator validates against the given request.
func makeReplyForRequest(req *radius.Packet, secret []byte) (*radius.Packet, []byte) {
	resp := &radius.Packet{
		Code:                 radius.CodeAccessAccept,
		Identifier:           req.Identifier,
		RequestAuthenticator: req.Authenticator,
		Secret:               secret,
	}
	wire, err := resp.Encode()
	if err != nil {
		panic(err)
	}
	parsed, err := radius.Decode(wire, secret)
	if err != nil {
		panic(err)
	}
	return parsed, wire
}

// makeReplyBadAuth builds a reply with a deliberately wrong authenticator.
func makeReplyBadAuth(identifier uint8) (*radius.Packet, []byte) {
	wire := make([]byte, 20)
	wire[0] = byte(radius.CodeAccessAccept)
	wire[1] = identifier
	wire[2] = 0
	wire[3] = 20
	for i := 4; i < 20; i++ {
		wire[i] = 0xAA
	}
	parsed, _ := radius.Decode(wire, []byte("dummy"))
	return parsed, wire
}

func TestReplyMatcher_RegisterAndDeliver(t *testing.T) {
	secret := []byte("test-secret")
	c := newFakeCollector()
	m := NewReplyMatcher(c)

	local := mkUDPAddr("127.0.0.1", 5000)
	remote := mkUDPAddr("127.0.0.1", 1812)

	req, err := radius.NewAccessRequestPAP(7, secret, "alice", "wonderland")
	require.NoError(t, err)

	ch := m.Register(local, 7, req, 42)
	reply, wire := makeReplyForRequest(req, secret)

	ok := m.Deliver(local, remote, reply, wire, secret, 100)
	require.True(t, ok, "matched delivery should return true")

	select {
	case rd := <-ch:
		require.Equal(t, radius.CodeAccessAccept, rd.Packet.Code)
		require.Equal(t, uint8(7), rd.Packet.Identifier)
	case <-time.After(time.Second):
		t.Fatalf("expected reply on channel")
	}

	require.Equal(t, 0, m.Inflight())
	// No error events should have been submitted on a clean match.
	require.Equal(t, 0, c.CountOf(events.EventTypeValidationFailed))
	require.Equal(t, 0, c.CountOf(events.EventTypeUnmatchedReply))
}

func TestReplyMatcher_DuplicateReplyDetected(t *testing.T) {
	secret := []byte("dupes")
	c := newFakeCollector()
	m := NewReplyMatcher(c)

	local := mkUDPAddr("127.0.0.1", 5001)
	remote := mkUDPAddr("127.0.0.1", 1812)

	req, err := radius.NewAccessRequestPAP(11, secret, "bob", "marley")
	require.NoError(t, err)

	ch := m.Register(local, 11, req, 99)
	reply, wire := makeReplyForRequest(req, secret)

	require.True(t, m.Deliver(local, remote, reply, wire, secret, 50))
	<-ch

	// Second delivery for same identifier — duplicate.
	dupOk := m.Deliver(local, remote, reply, wire, secret, 60)
	require.True(t, dupOk, "duplicate is still attributable (true)")

	require.Equal(t, 1, c.CountOf(events.EventTypeDuplicateReply))
	require.Equal(t, 0, c.CountOf(events.EventTypeUnmatchedReply))
}

func TestReplyMatcher_BadAuthenticatorRejected(t *testing.T) {
	secret := []byte("mismatch")
	c := newFakeCollector()
	m := NewReplyMatcher(c)

	local := mkUDPAddr("127.0.0.1", 5002)
	remote := mkUDPAddr("127.0.0.1", 1812)

	req, err := radius.NewAccessRequestPAP(33, secret, "carol", "rivers")
	require.NoError(t, err)

	ch := m.Register(local, 33, req, 5)
	bad, badWire := makeReplyBadAuth(33)
	ok := m.Deliver(local, remote, bad, badWire, secret, 0)
	require.False(t, ok, "bad authenticator should NOT count as matched")
	require.Equal(t, 1, c.CountOf(events.EventTypeValidationFailed))

	// Channel should NOT have received anything.
	select {
	case <-ch:
		t.Fatalf("bad-auth reply must not be delivered")
	case <-time.After(50 * time.Millisecond):
	}

	// Inflight should still be present so a legitimate reply later can match.
	require.Equal(t, 1, m.Inflight())

	// Now deliver the right one and confirm it still matches.
	good, goodWire := makeReplyForRequest(req, secret)
	require.True(t, m.Deliver(local, remote, good, goodWire, secret, 0))
	<-ch
}

func TestReplyMatcher_UnmatchedReply(t *testing.T) {
	secret := []byte("nope")
	c := newFakeCollector()
	m := NewReplyMatcher(c)

	local := mkUDPAddr("127.0.0.1", 5003)
	remote := mkUDPAddr("127.0.0.1", 1812)

	// No Register — straight Deliver.
	req, err := radius.NewAccessRequestPAP(0, secret, "x", "y")
	require.NoError(t, err)
	reply, wire := makeReplyForRequest(req, secret)

	ok := m.Deliver(local, remote, reply, wire, secret, 0)
	require.False(t, ok)
	require.Equal(t, 1, c.CountOf(events.EventTypeUnmatchedReply))
}

func TestReplyMatcher_CancelClearsEntry(t *testing.T) {
	secret := []byte("cancel")
	c := newFakeCollector()
	m := NewReplyMatcher(c)

	local := mkUDPAddr("127.0.0.1", 5004)
	req, err := radius.NewAccessRequestPAP(9, secret, "u", "p")
	require.NoError(t, err)

	_ = m.Register(local, 9, req, 0)
	require.Equal(t, 1, m.Inflight())

	m.Cancel(local, 9)
	require.Equal(t, 0, m.Inflight())

	// Deliver after cancel should be unmatched.
	reply, wire := makeReplyForRequest(req, secret)
	ok := m.Deliver(local, mkUDPAddr("127.0.0.1", 1812), reply, wire, secret, 0)
	require.False(t, ok)
}

func TestReplyMatcher_CloseAllReleasesInflight(t *testing.T) {
	c := newFakeCollector()
	m := NewReplyMatcher(c)

	secret := []byte("close")
	local := mkUDPAddr("127.0.0.1", 5005)
	for id := 0; id < 5; id++ {
		req, err := radius.NewAccessRequestPAP(uint8(id), secret, "u", "p")
		require.NoError(t, err)
		m.Register(local, uint8(id), req, uint32(id))
	}
	require.Equal(t, 5, m.Inflight())
	m.CloseAll()
	require.Equal(t, 0, m.Inflight())
}

func TestReplyMatcher_RegisterReplacesOld(t *testing.T) {
	c := newFakeCollector()
	m := NewReplyMatcher(c)
	secret := []byte("replace")
	local := mkUDPAddr("127.0.0.1", 5006)

	req1, err := radius.NewAccessRequestPAP(5, secret, "u", "p")
	require.NoError(t, err)
	old := m.Register(local, 5, req1, 1)

	req2, err := radius.NewAccessRequestPAP(5, secret, "u2", "p2")
	require.NoError(t, err)
	_ = m.Register(local, 5, req2, 2)

	// Delivering for req2 should match req2 (new), and the old waiter
	// should never receive — but its done channel was closed defensively.
	reply, wire := makeReplyForRequest(req2, secret)
	require.True(t, m.Deliver(local, mkUDPAddr("127.0.0.1", 1812), reply, wire, secret, 0))

	// Old channel should NOT have fired (req1 doesn't match req2's auth).
	select {
	case <-old:
		t.Fatalf("old waiter should not receive after replacement")
	case <-time.After(20 * time.Millisecond):
	}
}
