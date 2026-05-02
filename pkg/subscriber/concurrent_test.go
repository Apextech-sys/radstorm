// Package subscriber — concurrency + Pool-lookup race coverage.
//
// Purpose:
//
//	The OnCoA / OnDisconnect callbacks are invoked from the server
//	listener goroutine while the FSM may still be running its own
//	Run goroutine. These tests stress that boundary so `go test
//	-race` (when available) flags any unprotected state mutation.
//
//	Also exercises Pool lookups racing against a Pool.Range walk
//	to make sure the pool's internal RWMutex is honoured by every
//	accessor.
//
// Briefing: .orchestration/briefings/2b-subscriber.md
package subscriber

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// slowSender is a Sender that sleeps for `delay` before returning a
// scripted Access-Accept (or any chosen code). Used to keep the FSM
// in flight while concurrent OnCoA / OnDisconnect calls hammer it.
type slowSender struct {
	delay time.Duration
	code  radius.Code
}

func (s *slowSender) Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error) {
	pkt, err := build(0)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return &SendResult{Err: ctx.Err()}, nil
	}
	return &SendResult{
		Reply:       &radius.Packet{Code: s.code, Identifier: pkt.Identifier},
		ReplyBytes:  []byte{0x00},
		FirstSentAt: now,
		ReplyAt:     time.Now(),
		LocalAddr:   fakeAddr{s: "127.0.0.1:0"},
		RemoteAddr:  dst,
	}, nil
}

func TestConcurrent_OnCoAWhileRunning(t *testing.T) {
	cfg := baseConfig(false)
	cred := config.Credential{Username: "racey", Password: "x"}
	sub := New(1, cred, cfg)

	sender := &slowSender{delay: 20 * time.Millisecond, code: radius.CodeAccessAccept}
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}

	// Pre-stash deps so OnCoA arriving before Run starts still has a
	// collector to emit through. Run() will overwrite this — that's
	// fine, it's the same collector reference.
	sub.deps.Store(&deps)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := sub.Run(context.Background(), deps)
		require.NoError(t, err)
	}()

	const goroutines = 10
	const perGoroutine = 100
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				sub.OnCoA(&radius.Packet{Code: radius.CodeCoARequest, Identifier: byte(j)})
			}
		}()
	}
	wg.Wait()
	<-done

	require.Equal(t, int32(goroutines*perGoroutine), sub.CoaCount())
	require.Equal(t, StateEstablished, sub.State())
}

func TestConcurrent_OnDisconnectIdempotent(t *testing.T) {
	cfg := baseConfig(false)
	sub := New(2, config.Credential{Username: "kicked", Password: "x"}, cfg)

	sender := newFakeSender(scriptedReply{replyCode: radius.CodeAccessAccept})
	col := newFakeCollector()
	deps := Deps{Sender: sender, Collector: col, Config: cfg, Now: time.Now, T0: time.Now()}
	_, err := sub.Run(context.Background(), deps)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub.OnDisconnect(&radius.Packet{Code: radius.CodeDisconnectRequest})
		}()
	}
	wg.Wait()

	require.Equal(t, StateTerminated, sub.State())
	// 1 outcome from established + exactly 1 from the *first*
	// disconnect (the rest are no-ops).
	outs := col.snapshotOutcomes()
	require.Len(t, outs, 2)
}

func TestConcurrent_PoolLookupAndRange(t *testing.T) {
	cfg := baseConfig(false)
	cfg.Subscribers.Count = 64
	creds := make([]config.Credential, 64)
	for i := range creds {
		creds[i] = config.Credential{Username: byteName('a' + byte(i%26)), Password: "p"}
	}
	p := NewPool(creds, cfg)
	require.Equal(t, 64, p.Len())

	stop := make(chan struct{})
	var iter atomic.Int64

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				idx := uint32(iter.Add(1) % 64)
				if s := p.Get(idx); s != nil {
					_ = s.SessionID()
					_ = p.LookupByUsername(s.Username())
					_ = p.LookupBySessionID(s.SessionID())
					_ = p.LookupByFramedIP(s.FramedIP().String())
				}
				p.Range(func(*Subscriber) {})
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
	require.Greater(t, iter.Load(), int64(0))
}
