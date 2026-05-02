// Package io — Engine.Send / sender tests.
//
// Purpose:
//
//	End-to-end tests of the public Send API against in-process echo
//	servers. Covers happy-path round-trip, retransmit on black-hole,
//	recovery after partial black-hole, ID exhaustion, context cancel,
//	and event emission ordering.
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

func newTestEngine(t *testing.T, secret []byte, c Collector) *Engine {
	t.Helper()
	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: c,
		// Tight settings so tests run fast.
		AllocBackoffStep: 1 * time.Millisecond,
		AllocMaxWait:     20 * time.Millisecond,
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	return e
}

func TestEngine_Send_HappyPath(t *testing.T) {
	secret := []byte("happy")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	policy := RetransmitPolicy{
		InitialTimeout: 2 * time.Second,
		MaxRetries:     2,
		Backoff:        BackoffConstant,
		BackoffBase:    500 * time.Millisecond,
	}

	res, err := e.Send(context.Background(), srv.Addr(),
		buildSimpleAccessRequest(secret, "happy", "user"), policy, 1)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, radius.CodeAccessAccept, res.Reply.Code)
	require.Equal(t, 0, res.RetransmitN, "loopback echo should not require retransmit")
	require.Greater(t, res.LatencyUs, int64(0))

	// Events: one request_sent, one reply_received. (No retransmit
	// expected on the local-loopback echo path.)
	require.Equal(t, 1, c.CountOf(events.EventTypeRequestSent))
	require.Equal(t, 1, c.CountOf(events.EventTypeReplyReceived))
	require.Equal(t, 0, c.CountOf(events.EventTypeRequestRetransmitted))
}

func TestEngine_Send_RetransmitsOnBlackHole(t *testing.T) {
	secret := []byte("dark")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeBlackHole)

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	policy := RetransmitPolicy{
		InitialTimeout: 50 * time.Millisecond,
		MaxRetries:     2,
		Backoff:        BackoffConstant,
		BackoffBase:    25 * time.Millisecond,
	}

	res, err := e.Send(context.Background(), srv.Addr(),
		buildSimpleAccessRequest(secret, "u", "p"), policy, 7)
	require.Nil(t, res)
	require.ErrorIs(t, err, ErrFinalTimeout)

	// Original + 2 retransmits = 3 packets sent.
	require.Equal(t, int64(3), srv.RxCount(),
		"expected 3 transmissions before giving up")

	require.Equal(t, 1, c.CountOf(events.EventTypeRequestSent))
	require.Equal(t, 2, c.CountOf(events.EventTypeRequestRetransmitted))
	require.Equal(t, 0, c.CountOf(events.EventTypeReplyReceived))
}

func TestEngine_Send_RecoversAfterPartialBlackHole(t *testing.T) {
	secret := []byte("recover")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeDelayed)
	// First two packets are delayed past the per-attempt timeout, so
	// they trigger retransmits; third attempt gets an immediate reply.
	srv.SetDelay(500*time.Millisecond, 2)

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	policy := RetransmitPolicy{
		InitialTimeout: 75 * time.Millisecond,
		MaxRetries:     3,
		Backoff:        BackoffConstant,
		BackoffBase:    50 * time.Millisecond,
	}

	res, err := e.Send(context.Background(), srv.Addr(),
		buildSimpleAccessRequest(secret, "r", "p"), policy, 13)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 2, res.RetransmitN, "should succeed on retransmit #2")

	require.Equal(t, 1, c.CountOf(events.EventTypeRequestSent))
	require.Equal(t, 2, c.CountOf(events.EventTypeRequestRetransmitted))
	require.Equal(t, 1, c.CountOf(events.EventTypeReplyReceived))
}

func TestEngine_Send_NewIdentifierPerRetransmit(t *testing.T) {
	secret := []byte("ids")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeBlackHole)

	var seenIDs []uint8
	var mu sync.Mutex
	srv.SetOnRequest(func(p *radius.Packet, _ *net.UDPAddr) {
		mu.Lock()
		seenIDs = append(seenIDs, p.Identifier)
		mu.Unlock()
	})

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	policy := RetransmitPolicy{
		InitialTimeout: 30 * time.Millisecond,
		MaxRetries:     2,
		Backoff:        BackoffConstant,
		BackoffBase:    20 * time.Millisecond,
	}

	_, err := e.Send(context.Background(), srv.Addr(),
		buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
	require.ErrorIs(t, err, ErrFinalTimeout)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seenIDs, 3)
	uniq := map[uint8]struct{}{}
	for _, id := range seenIDs {
		uniq[id] = struct{}{}
	}
	require.Len(t, uniq, 3, "RFC requires a NEW identifier per retransmit; got %v", seenIDs)
}

func TestEngine_Send_ContextCancelStopsImmediately(t *testing.T) {
	secret := []byte("cancel")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeBlackHole)

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	ctx, cancel := context.WithCancel(context.Background())
	policy := RetransmitPolicy{
		InitialTimeout: 5 * time.Second,
		MaxRetries:     5,
		Backoff:        BackoffConstant,
		BackoffBase:    1 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		_, err := e.Send(ctx, srv.Addr(),
			buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
		done <- err
	}()

	time.Sleep(40 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled),
			"expected context.Canceled, got %v", err)
	case <-time.After(time.Second):
		t.Fatalf("Send did not honour ctx cancel")
	}
}

func TestEngine_Send_IdentifierExhaustion(t *testing.T) {
	secret := []byte("exhaust")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeBlackHole)

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)
	dst := srv.Addr()

	// Pre-allocate every ID for the only (src, dst) tuple to simulate
	// "all 256 in flight".
	localUDP := e.LocalAddrs()[0]
	tuple := MakeTuple(localUDP, dst)
	for i := 0; i < 256; i++ {
		_, ok := e.idAlloc.Allocate(tuple)
		require.True(t, ok)
	}

	policy := RetransmitPolicy{
		InitialTimeout: 100 * time.Millisecond,
		MaxRetries:     0,
	}
	_, err := e.Send(context.Background(), dst,
		buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
	require.ErrorIs(t, err, ErrIdentifierExhausted)
}

func TestEngine_Send_BadAuthIsNotAccepted(t *testing.T) {
	secret := []byte("badauth")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeBadAuth)

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	policy := RetransmitPolicy{
		InitialTimeout: 50 * time.Millisecond,
		MaxRetries:     1,
		Backoff:        BackoffConstant,
		BackoffBase:    25 * time.Millisecond,
	}
	_, err := e.Send(context.Background(), srv.Addr(),
		buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
	require.ErrorIs(t, err, ErrFinalTimeout,
		"bad-auth replies must not satisfy Send; final timeout expected")

	require.Greater(t, c.CountOf(events.EventTypeValidationFailed), 0)
	require.Equal(t, 0, c.CountOf(events.EventTypeReplyReceived))
}

func TestEngine_Send_DuplicateReplyEmitsEvent(t *testing.T) {
	secret := []byte("dup")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()
	srv.SetMode(modeReplyTwice)

	c := newFakeCollector()
	e := newTestEngine(t, secret, c)

	policy := RetransmitPolicy{
		InitialTimeout: 200 * time.Millisecond,
		MaxRetries:     0,
	}
	res, err := e.Send(context.Background(), srv.Addr(),
		buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Give the second reply a moment to arrive and be classified.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.CountOf(events.EventTypeDuplicateReply) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, c.CountOf(events.EventTypeDuplicateReply), 1,
		"second reply should be classified as duplicate")
	require.Equal(t, 1, c.CountOf(events.EventTypeReplyReceived))
}

func TestEngine_Send_BeforeStartFails(t *testing.T) {
	secret := []byte("nostart")
	e, err := NewEngine(Opts{
		SourceIPs: []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:    secret,
		Collector: newFakeCollector(),
	})
	require.NoError(t, err)
	defer e.Stop(context.Background())

	policy := RetransmitPolicy{InitialTimeout: 10 * time.Millisecond}
	_, err = e.Send(context.Background(), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1812},
		buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
	require.ErrorIs(t, err, ErrNotStarted)
}

func TestEngine_Send_IDsReleasedAfterReply(t *testing.T) {
	secret := []byte("release")
	srv := newTestEchoServer(t, secret)
	defer srv.Close()

	c := newFakeCollector()
	// Use a larger AllocMaxWait so sends queueing up while the 256 ID
	// slots churn don't bail out — we WANT them to wait for an ID.
	e, err := NewEngine(Opts{
		SourceIPs:        []net.IP{net.IPv4(127, 0, 0, 1)},
		Secret:           secret,
		Collector:        c,
		AllocBackoffStep: 1 * time.Millisecond,
		AllocMaxWait:     2 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, e.Start(context.Background()))
	defer e.Stop(context.Background())

	policy := RetransmitPolicy{InitialTimeout: 500 * time.Millisecond}

	// Run sequentially-batched concurrent sends. We use 300 > 256 so
	// some Allocate calls have to wait for prior sends to release —
	// that's the contract we're verifying.
	const N = 300
	var wg sync.WaitGroup
	var failures atomic.Int32
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := e.Send(context.Background(), srv.Addr(),
				buildSimpleAccessRequest(secret, "u", "p"), policy, 0)
			if err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int32(0), failures.Load(),
		"all %d sends should succeed if IDs are released cleanly", N)

	// Inflight should now be zero.
	localUDP := e.LocalAddrs()[0]
	tuple := MakeTuple(localUDP, srv.Addr())
	require.Equal(t, 0, e.idAlloc.Inflight(tuple))
}
