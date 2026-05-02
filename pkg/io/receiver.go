// Package io — per-socket inbound reader.
//
// Purpose:
//
//	Receiver launches one goroutine per UDP socket. Each goroutine reads
//	datagrams, decodes them via radius.Decode, and dispatches them:
//	  - Reply codes (Access-Accept/Reject, Accounting-Response,
//	    CoA-ACK/NAK, Disconnect-ACK/NAK) → ReplyMatcher.Deliver.
//	  - Server-initiated codes (CoA-Request, Disconnect-Request) → the
//	    registered ServerHandler (the pkg/server listener registers
//	    itself).
//	  - Anything else → unmatched_reply event.
//
// Related files:
//   - pkg/io/socketpool.go (sockets to read from)
//   - pkg/io/matcher.go    (Deliver target for replies)
//   - pkg/io/io.go         (ServerHandler interface)
//   - pkg/radius/packet.go (Decode)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: internal — Receiver is not directly invoked by callers; the
// Engine starts/stops it.
package io

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// Receiver reads inbound datagrams off every socket in the pool.
type Receiver struct {
	pool      *SocketPool
	matcher   *ReplyMatcher
	handler   ServerHandler
	secret    []byte
	collector Collector
	bufBytes  int

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopped  atomic.Bool

	startedAt atomic.Int64 // unix nanos
}

// NewReceiver constructs a Receiver. Start launches goroutines.
func NewReceiver(pool *SocketPool, matcher *ReplyMatcher, handler ServerHandler, secret []byte, collector Collector, bufBytes int) *Receiver {
	if collector == nil {
		collector = nopCollector{}
	}
	if bufBytes <= 0 {
		bufBytes = radius.MaxPacketLength
	}
	return &Receiver{
		pool:      pool,
		matcher:   matcher,
		handler:   handler,
		secret:    secret,
		collector: collector,
		bufBytes:  bufBytes,
	}
}

// Start launches one goroutine per socket. Goroutines exit on socket
// close (which Stop / Engine.Stop triggers) or when ctx is cancelled.
func (r *Receiver) Start(ctx context.Context) {
	if !r.startedAt.CompareAndSwap(0, nowUnixNano()) {
		return
	}
	for _, s := range r.pool.Sockets() {
		s := s
		r.wg.Add(1)
		go r.readLoop(ctx, s)
	}
	// Watcher goroutine: if ctx is cancelled, close sockets so blocked
	// reads return.
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		<-ctx.Done()
		r.Stop()
	}()
}

// Stop signals goroutines to exit by closing the sockets. Idempotent.
// Does NOT wait — Engine.Stop calls Wait via the receiver's internal
// waitgroup indirectly through pool.Close + this method.
func (r *Receiver) Stop() {
	r.stopOnce.Do(func() {
		r.stopped.Store(true)
		// pool.Close drives the read loops out of net.UDPConn.ReadFromUDP.
		r.pool.Close()
	})
}

// Wait blocks until all read goroutines have exited.
func (r *Receiver) Wait() {
	r.wg.Wait()
}

// readLoop drives one socket. Exits on ReadFrom error (socket closed).
func (r *Receiver) readLoop(ctx context.Context, s *net.UDPConn) {
	defer r.wg.Done()

	buf := make([]byte, r.bufBytes)
	localAddr := s.LocalAddr()

	for {
		n, remoteAddr, err := s.ReadFromUDP(buf)
		if err != nil {
			// Distinguish "closed by us" from real errors. Either way,
			// exit the loop — the socket is unusable.
			if r.stopped.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient errors aren't worth re-trying with a closed
			// socket; the engine will eventually be torn down. Emit an
			// internal_error event so the operator sees it and exit.
			r.collector.Submit(events.NewError(
				0,
				r.offsetMs(),
				events.EventTypeInternalError,
				0,
				"udp read: "+err.Error(),
			))
			return
		}

		// Copy out the datagram bytes; downstream code needs a stable
		// slice (validation re-zeros the MA position on a copy, etc.).
		wire := make([]byte, n)
		copy(wire, buf[:n])

		r.dispatch(localAddr, remoteAddr, wire)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// dispatch decodes a datagram and routes it.
func (r *Receiver) dispatch(localAddr net.Addr, remoteAddr *net.UDPAddr, wire []byte) {
	pkt, err := radius.Decode(wire, r.secret)
	if err != nil {
		r.collector.Submit(events.NewError(
			0,
			r.offsetMs(),
			events.EventTypeValidationFailed,
			0,
			"radius decode: "+err.Error(),
		))
		return
	}

	switch {
	case pkt.Code.IsResponse():
		r.matcher.Deliver(localAddr, remoteAddr, pkt, wire, r.secret, r.offsetMs())
	case pkt.Code == radius.CodeCoARequest || pkt.Code == radius.CodeDisconnectRequest:
		if r.handler != nil {
			r.handler.HandleServerInitiated(pkt, wire, remoteAddr, localAddr)
			return
		}
		// No handler: drop with an event so the operator notices.
		var et string
		if pkt.Code == radius.CodeCoARequest {
			et = events.EventTypeCoADropped
			r.collector.Submit(events.NewCoAEvent(
				0, r.offsetMs(), et, int16(pkt.Identifier),
				localAddr.String(), remoteAddr.String(), int32(len(wire)),
				0, 0,
			))
		} else {
			r.collector.Submit(events.NewDisconnectEvent(
				0, r.offsetMs(), events.EventTypeDisconnectReceived, int16(pkt.Identifier),
				localAddr.String(), remoteAddr.String(), int32(len(wire)),
				0, 0,
			))
		}
	default:
		// Unexpected: a request code we don't expect (Access-Request
		// inbound to us, etc.). Treat as unmatched.
		r.collector.Submit(events.NewUnmatchedReply(
			r.offsetMs(),
			int8(pkt.Code),
			int16(pkt.Identifier),
			localAddr.String(),
			remoteAddr.String(),
			int32(len(wire)),
		))
	}
}

// offsetMs returns ms since Receiver.Start.
func (r *Receiver) offsetMs() int64 {
	t := r.startedAt.Load()
	if t == 0 {
		return 0
	}
	return (nowUnixNano() - t) / int64Million
}

// constants used to avoid pulling time.* in this hot path needlessly.
const int64Million = 1_000_000

// nowUnixNano is split out so tests can override (not currently used).
var nowUnixNano = func() int64 {
	return timeNowUnixNano()
}
