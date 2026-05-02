// Package io — in-process UDP echo / black-hole helpers for tests.
//
// Purpose:
//
//	Spins up a tiny UDP server bound to a kernel-chosen port that
//	decodes inbound Access-Request packets and replies with an
//	Access-Accept whose Response Authenticator is computed against the
//	request's RequestAuthenticator + the shared secret. Modes let tests
//	exercise: normal echo, black-hole (never reply), delayed reply
//	(sleep N before replying), reply-twice (test duplicate detection),
//	bad-authenticator (test validation rejection).
//
//	This avoids needing the Docker FreeRADIUS rig for unit tests while
//	keeping the wire-level test loop honest.
//
// Related files:
//   - pkg/io/sender_test.go (consumer)
//   - pkg/io/matcher_test.go (consumer)
//   - pkg/io/receiver_test.go (consumer)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// echoMode controls how the test server responds.
type echoMode int

const (
	// modeEcho replies with Access-Accept immediately.
	modeEcho echoMode = iota
	// modeBlackHole never replies.
	modeBlackHole
	// modeDelayed replies after `delay` for the first `delayCount` packets,
	// then replies immediately for the rest.
	modeDelayed
	// modeReplyTwice replies twice (to exercise duplicate detection).
	modeReplyTwice
	// modeBadAuth replies with an Access-Accept whose authenticator is
	// random garbage (to exercise validation_failed events).
	modeBadAuth
)

// testEchoServer is a tiny UDP RADIUS responder used by sender / matcher /
// receiver tests. Construct via newTestEchoServer; call Close on cleanup.
type testEchoServer struct {
	conn       *net.UDPConn
	secret     []byte
	mode       atomic.Int32 // echoMode
	delay      time.Duration
	delayCount atomic.Int32
	rxCount    atomic.Int64

	wg          sync.WaitGroup
	stopOnce    sync.Once
	closeSignal atomic.Bool

	// onRequest is called (under no lock) for every received request,
	// useful for tests that want to assert what we received.
	onRequest func(*radius.Packet, *net.UDPAddr)
}

func newTestEchoServer(t *testing.T, secret []byte) *testEchoServer {
	t.Helper()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	s := &testEchoServer{
		conn:   conn,
		secret: append([]byte(nil), secret...),
	}
	s.mode.Store(int32(modeEcho))
	s.wg.Add(1)
	go s.serve()
	return s
}

// Addr returns the listening UDP address (suitable as Engine.Send dst).
func (s *testEchoServer) Addr() *net.UDPAddr {
	return s.conn.LocalAddr().(*net.UDPAddr)
}

// SetMode switches the response mode at runtime.
func (s *testEchoServer) SetMode(m echoMode) { s.mode.Store(int32(m)) }

// SetDelay configures the delay used in modeDelayed and how many
// packets get the delay treatment before reverting to immediate replies.
func (s *testEchoServer) SetDelay(d time.Duration, count int) {
	s.delay = d
	s.delayCount.Store(int32(count))
}

// RxCount returns the number of requests this server has decoded.
func (s *testEchoServer) RxCount() int64 { return s.rxCount.Load() }

// Close shuts the server down.
func (s *testEchoServer) Close() {
	s.stopOnce.Do(func() {
		s.closeSignal.Store(true)
		_ = s.conn.Close()
	})
	s.wg.Wait()
}

func (s *testEchoServer) serve() {
	defer s.wg.Done()
	buf := make([]byte, radius.MaxPacketLength)
	for {
		n, src, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if s.closeSignal.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		wire := append([]byte(nil), buf[:n]...)
		req, err := radius.Decode(wire, s.secret)
		if err != nil {
			continue
		}
		s.rxCount.Add(1)
		if s.onRequest != nil {
			s.onRequest(req, src)
		}
		mode := echoMode(s.mode.Load())
		// Hand each request off to its own goroutine so a delayed-reply
		// mode doesn't head-of-line block subsequent packets in the
		// shared receive loop.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(mode, req, src)
		}()
	}
}

func (s *testEchoServer) handle(mode echoMode, req *radius.Packet, src *net.UDPAddr) {
	switch mode {
	case modeBlackHole:
		return
	case modeDelayed:
		remaining := s.delayCount.Add(-1)
		if remaining >= 0 {
			time.Sleep(s.delay)
		}
		s.replyAccept(req, src)
	case modeReplyTwice:
		s.replyAccept(req, src)
		s.replyAccept(req, src)
	case modeBadAuth:
		s.replyBadAuth(req, src)
	default:
		s.replyAccept(req, src)
	}
}

// replyAccept builds and writes an Access-Accept whose authenticator is
// computed correctly from the request's authenticator + shared secret.
func (s *testEchoServer) replyAccept(req *radius.Packet, dst *net.UDPAddr) {
	resp := &radius.Packet{
		Code:                 radius.CodeAccessAccept,
		Identifier:           req.Identifier,
		RequestAuthenticator: req.Authenticator,
		Secret:               s.secret,
	}
	wire, err := resp.Encode()
	if err != nil {
		return
	}
	_, _ = s.conn.WriteToUDP(wire, dst)
}

// replyBadAuth writes an Access-Accept with a random authenticator that
// won't validate.
func (s *testEchoServer) replyBadAuth(req *radius.Packet, dst *net.UDPAddr) {
	wire := make([]byte, 20)
	wire[0] = byte(radius.CodeAccessAccept)
	wire[1] = req.Identifier
	wire[2] = 0
	wire[3] = 20
	for i := 4; i < 20; i++ {
		wire[i] = 0xAA
	}
	_, _ = s.conn.WriteToUDP(wire, dst)
}

// fakeCollector captures submitted events for assertion in tests.
type fakeCollector struct {
	mu     sync.Mutex
	events []events.Event
}

func newFakeCollector() *fakeCollector { return &fakeCollector{} }

// Submit implements Collector.
func (f *fakeCollector) Submit(e events.Event) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}

// Snapshot returns a copy of the events submitted so far.
func (f *fakeCollector) Snapshot() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]events.Event, len(f.events))
	copy(out, f.events)
	return out
}

// Count returns the number of events submitted so far.
func (f *fakeCollector) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// CountOf returns the number of events with the given event type.
func (f *fakeCollector) CountOf(eventType string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.EventType == eventType {
			n++
		}
	}
	return n
}

// HasEvent reports whether any event with the given type was submitted.
func (f *fakeCollector) HasEvent(eventType string) bool {
	return f.CountOf(eventType) > 0
}

// buildSimpleAccessRequest is a convenient packet builder used by tests
// that don't care about specific attributes.
func buildSimpleAccessRequest(secret []byte, username, password string) func(id uint8) (*radius.Packet, error) {
	return func(id uint8) (*radius.Packet, error) {
		return radius.NewAccessRequestPAP(id, secret, username, password)
	}
}
