// Package server — RFC 5176 CoA + Disconnect listener for radstorm.
//
// Purpose:
//
//	Owns the UDP socket that receives server-initiated CoA-Request and
//	Disconnect-Request packets from the RADIUS server under test. Spawns
//	N reader goroutines that decode each datagram and dispatch it to the
//	handler. Owns lifecycle (Start/Stop) and connection cleanup.
//
// Related files:
//   - pkg/server/handler.go (per-packet validation + ACK/NAK construction)
//   - pkg/server/lookup.go  (SubscriberLookup / SubscriberTarget interfaces)
//   - pkg/radius             (packet encode/decode, constructors)
//   - pkg/events             (event constructors)
//
// Briefing: .orchestration/briefings/2c-server-listener.md
//
// Contract: Public — Listener (with Opts, New, Start, Stop) is the only
// thing scenario code is expected to instantiate from this package.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Opts configures a Listener. Zero values are replaced with sane defaults
// documented next to each field.
type Opts struct {
	// BindAddress is the host:port the UDP socket binds to. Required.
	// Use ":3799" to listen on all interfaces on the IANA-assigned
	// dynamic-authorization port.
	BindAddress string

	// SharedSecret is the RADIUS shared secret used to validate
	// Message-Authenticator on inbound packets and to compute the
	// Response Authenticator + Message-Authenticator on outbound replies.
	// Required.
	SharedSecret []byte

	// Lookup resolves an inbound packet to the targeted subscriber.
	// Required.
	Lookup SubscriberLookup

	// Collector receives events emitted by the listener. Required.
	Collector Collector

	// ReaderCount is the number of goroutines reading from the UDP
	// socket in parallel. Defaults to runtime.NumCPU(). One is enough
	// for low CoA volumes; multiple lets the kernel fan out under storm.
	ReaderCount int

	// ReadBufferSize is the SetReadBuffer hint passed to the OS. 0 leaves
	// the system default in place. Useful to bump under burst load.
	ReadBufferSize int

	// TestT0 is the zero-time anchor used to compute Event.OffsetMs. If
	// zero, time.Now() is captured at New(). Tests pin this so latency
	// math is deterministic.
	TestT0 time.Time

	// Logger is the slog logger used for operational messages (failed
	// reads, write errors). If nil, slog.Default() is used. Per
	// docs/ARCHITECTURE.md, observable test events go to the Collector
	// — slog is for harness-operational messages only.
	Logger *slog.Logger
}

// Listener is the radstorm CoA / Disconnect server. It binds one UDP
// socket and demultiplexes inbound dynamic-authorization packets to the
// per-packet handler.
type Listener struct {
	opts   Opts
	conn   *net.UDPConn
	logger *slog.Logger

	t0 time.Time

	startOnce sync.Once
	stopOnce  sync.Once
	stopped   atomic.Bool
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// localAddr is captured after Listen so events can record it even
	// after Stop has closed the conn.
	localAddr string

	// counters — atomic so tests can read them without racing.
	pktReceived atomic.Int64
	pktAcked    atomic.Int64
	pktNaked    atomic.Int64
	pktDropped  atomic.Int64
}

// Errors returned by New / Start / Stop.
var (
	ErrBindAddressRequired = errors.New("server: BindAddress is required")
	ErrSecretRequired      = errors.New("server: SharedSecret is required")
	ErrLookupRequired      = errors.New("server: Lookup is required")
	ErrCollectorRequired   = errors.New("server: Collector is required")
	ErrAlreadyStarted      = errors.New("server: listener already started")
	ErrAlreadyStopped      = errors.New("server: listener already stopped")
)

// New constructs a Listener and binds its UDP socket. Returns an error
// if any required Opts field is missing or the bind fails.
//
// New does NOT start reader goroutines — call Start(ctx) for that. We
// split bind from start so callers can fail fast on a port collision
// before scheduling the rest of the run.
func New(opts Opts) (*Listener, error) {
	if opts.BindAddress == "" {
		return nil, ErrBindAddressRequired
	}
	if len(opts.SharedSecret) == 0 {
		return nil, ErrSecretRequired
	}
	if opts.Lookup == nil {
		return nil, ErrLookupRequired
	}
	if opts.Collector == nil {
		return nil, ErrCollectorRequired
	}
	if opts.ReaderCount <= 0 {
		opts.ReaderCount = runtime.NumCPU()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	udpAddr, err := net.ResolveUDPAddr("udp", opts.BindAddress)
	if err != nil {
		return nil, fmt.Errorf("server: resolve bind address %q: %w", opts.BindAddress, err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("server: listen on %s: %w", opts.BindAddress, err)
	}

	if opts.ReadBufferSize > 0 {
		if err := conn.SetReadBuffer(opts.ReadBufferSize); err != nil {
			// Non-fatal — log and continue with the kernel default.
			opts.Logger.Warn("server: SetReadBuffer failed", "size", opts.ReadBufferSize, "err", err)
		}
	}

	t0 := opts.TestT0
	if t0.IsZero() {
		t0 = time.Now()
	}

	return &Listener{
		opts:      opts,
		conn:      conn,
		logger:    opts.Logger,
		t0:        t0,
		localAddr: conn.LocalAddr().String(),
		stopCh:    make(chan struct{}),
	}, nil
}

// LocalAddr returns the UDP address the Listener is bound to as a string.
// Useful in tests so the in-process client knows where to send.
func (l *Listener) LocalAddr() string { return l.localAddr }

// Start spawns ReaderCount goroutines that read inbound datagrams. Returns
// ErrAlreadyStarted on the second call. Does not block — caller drives
// shutdown via Stop or by cancelling ctx.
//
// When ctx is cancelled the listener closes the UDP socket so readers
// unblock from ReadFromUDP with net.ErrClosed and exit. The cancel
// watcher itself listens on either ctx.Done OR the listener's internal
// stopCh so Stop can also unblock it directly.
func (l *Listener) Start(ctx context.Context) error {
	started := false
	l.startOnce.Do(func() {
		started = true
		for i := 0; i < l.opts.ReaderCount; i++ {
			l.wg.Add(1)
			go l.readLoop(ctx)
		}
		// Cancel watcher: if ctx fires OR Stop fires, close the conn so
		// reads return. Listening on stopCh is what lets Stop unblock
		// this goroutine even when ctx is context.Background().
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			select {
			case <-ctx.Done():
			case <-l.stopCh:
			}
			_ = l.closeConn()
		}()
	})
	if !started {
		return ErrAlreadyStarted
	}
	return nil
}

// Stop closes the UDP socket and waits for reader goroutines to exit.
// Returns ErrAlreadyStopped on the second call. ctx bounds the wait;
// when ctx fires Stop returns even if a reader is still in flight (it
// will eventually exit on its own when the socket close completes).
func (l *Listener) Stop(ctx context.Context) error {
	var stopErr error
	stopped := false
	l.stopOnce.Do(func() {
		stopped = true
		l.stopped.Store(true)
		// Signal the cancel-watcher goroutine (if Start was ever called)
		// to release its select; it will then close the conn for us.
		close(l.stopCh)
		// Belt-and-braces: also close the conn directly so even if Start
		// was never called the readers (none in that case) and the conn
		// resources are released.
		_ = l.closeConn()

		done := make(chan struct{})
		go func() {
			l.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			stopErr = fmt.Errorf("server: stop wait: %w", ctx.Err())
		}
	})
	if !stopped {
		return ErrAlreadyStopped
	}
	return stopErr
}

// closeConn closes the underlying UDP socket. Safe to call multiple times
// from different goroutines — extra calls return an "already closed" error
// which we swallow.
func (l *Listener) closeConn() error {
	if l.conn == nil {
		return nil
	}
	err := l.conn.Close()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// readLoop is the per-reader hot path. Reads one datagram, captures the
// monotonic receive time, then hands off to handlePacket. The receive
// time is critical: it's the start of the latency measurement, so we
// must capture it BEFORE any decode work.
func (l *Listener) readLoop(ctx context.Context) {
	defer l.wg.Done()
	// 4096 is RADIUS MaxPacketLength. One read = one packet.
	buf := make([]byte, 4096)

	for {
		if l.stopped.Load() {
			return
		}
		n, remoteAddr, err := l.conn.ReadFromUDP(buf)
		// Capture monotonic time the moment the read returns, BEFORE
		// we copy bytes or do any other work. This is the canonical
		// "received at" reference for the response-latency event.
		recvAt := time.Now()

		if err != nil {
			if l.stopped.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient error — log and keep reading. ctx may be done
			// (Stop was called) in which case the next iteration sees
			// stopped.
			l.logger.Warn("server: udp read error", "err", err)
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		if n == 0 {
			continue
		}

		// Copy the bytes — the next iteration overwrites buf.
		pkt := make([]byte, n)
		copy(pkt, buf[:n])

		l.pktReceived.Add(1)
		l.handlePacket(pkt, remoteAddr, recvAt)
	}
}
