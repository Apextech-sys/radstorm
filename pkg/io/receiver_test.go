// Package io — Receiver / ServerHandler tests.
//
// Purpose:
//   Verifies that inbound CoA-Request and Disconnect-Request datagrams
//   reach the registered ServerHandler, that absent-handler drops emit
//   the expected event, and that decode errors emit a validation_failed
//   event.
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// sendCoARequestTo crafts a CoA-Request and writes it to the given dst.
// Used to drive the Engine receiver.
func sendCoARequestTo(t *testing.T, secret []byte, dst *net.UDPAddr) {
	t.Helper()

	pkt := &radius.Packet{
		Code:       radius.CodeCoARequest,
		Identifier: 99,
		Secret:     secret,
	}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "victim"))
	require.NoError(t, pkt.Attributes.Add(radius.AttrMessageAuthenticator, make([]byte, radius.MessageAuthenticatorLength)))
	wire, err := pkt.Encode()
	require.NoError(t, err)

	src, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer src.Close()
	_, err = src.WriteToUDP(wire, dst)
	require.NoError(t, err)
}

func TestReceiver_RoutesCoAToHandler(t *testing.T) {
	secret := []byte("coa")
	c := newFakeCollector()

	gotCoA := make(chan *radius.Packet, 1)
	handler := ServerHandlerFunc(func(p *radius.Packet, wire []byte, src net.Addr, local net.Addr) {
		gotCoA <- p
	})

	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: c,
		Handler:   handler,
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	defer e.Stop(context.Background())

	dst := e.LocalAddrs()[0]
	sendCoARequestTo(t, secret, dst)

	select {
	case p := <-gotCoA:
		require.Equal(t, radius.CodeCoARequest, p.Code)
		require.Equal(t, uint8(99), p.Identifier)
	case <-time.After(time.Second):
		t.Fatalf("handler never invoked for CoA-Request")
	}
}

func TestReceiver_DropsCoAWithoutHandler(t *testing.T) {
	secret := []byte("nohandler")
	c := newFakeCollector()

	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: c,
		// Handler is nil.
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	defer e.Stop(context.Background())

	dst := e.LocalAddrs()[0]
	sendCoARequestTo(t, secret, dst)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.CountOf(events.EventTypeCoADropped) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, c.CountOf(events.EventTypeCoADropped), 1,
		"absent handler must emit coa_dropped")
}

func TestReceiver_BadDatagramEmitsValidationFailed(t *testing.T) {
	secret := []byte("bad")
	c := newFakeCollector()

	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: c,
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	defer e.Stop(context.Background())

	src, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer src.Close()

	// 5-byte garbage — too short to be a valid RADIUS packet.
	garbage := []byte{1, 2, 3, 4, 5}
	_, err = src.WriteToUDP(garbage, e.LocalAddrs()[0])
	require.NoError(t, err)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.CountOf(events.EventTypeValidationFailed) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, c.CountOf(events.EventTypeValidationFailed), 1)
}

func TestEngine_Stop_IsIdempotentAndStopsReceiver(t *testing.T) {
	secret := []byte("stop")
	c := newFakeCollector()
	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: c,
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))

	require.NoError(t, e.Stop(context.Background()))
	require.NoError(t, e.Stop(context.Background()), "Stop must be idempotent")

	// Send after Stop should fail (engine flagged stopped, sockets closed).
	policy := RetransmitPolicy{InitialTimeout: 10 * time.Millisecond}
	_, err = e.Send(context.Background(), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1812},
		buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
	require.Error(t, err)
}

func TestNewEngine_RejectsBadInputs(t *testing.T) {
	_, err := NewEngine(Opts{Secret: []byte("x")})
	require.ErrorIs(t, err, ErrNoSockets)

	_, err = NewEngine(Opts{SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)}})
	require.Error(t, err, "missing secret rejected")
}

func TestEngine_Start_TwiceIsErr(t *testing.T) {
	secret := []byte("dbl")
	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: newFakeCollector(),
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	defer e.Stop(context.Background())
	require.ErrorIs(t, e.Start(context.Background()), ErrAlreadyStarted)
}

func TestEngine_StartAfterStopFails(t *testing.T) {
	secret := []byte("aft")
	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: newFakeCollector(),
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	require.NoError(t, e.Stop(context.Background()))
	// Starting a stopped engine must error.
	require.Error(t, e.Start(context.Background()))
}

func TestEngine_NopCollectorWhenAbsent(t *testing.T) {
	secret := []byte("nopc")
	// Build with explicit nil collector — engine should swap in nopCollector.
	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: nil,
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	defer e.Stop(context.Background())

	// Send a CoA packet to confirm the receiver dispatches without panicking
	// even with the nop collector.
	dst := e.LocalAddrs()[0]
	sendCoARequestTo(t, secret, dst)
	time.Sleep(50 * time.Millisecond)
}

// quiet a static-analysis nag when a test only calls atomic.* via the
// helper closures; ensures the import remains exercised even if no test
// in this file directly references the package.
var _ = atomic.Int32{}
