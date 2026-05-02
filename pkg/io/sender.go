// Package io — RADIUS request sender with retransmit lifecycle.
//
// Purpose:
//
//	runSend orchestrates a single Send call:
//	  1. Pick a local socket whose tuple has a free Identifier.
//	  2. Allocate a fresh Identifier.
//	  3. Have the caller's `build` closure construct the request packet.
//	  4. Encode and write the packet to the wire.
//	  5. Register with the matcher; emit `request_sent` event.
//	  6. Wait on the matcher's reply channel with the per-attempt
//	     timeout. On reply: emit `reply_received`, release the ID,
//	     return SendResult.
//	  7. On timeout: cancel the matcher entry, RELEASE the old ID,
//	     allocate a NEW one (RFC-correct, never reuse on retransmit),
//	     emit `request_retransmitted`, repeat.
//	  8. On exhausted retries / context cancellation: release any held
//	     ID and return.
//
// Related files:
//   - pkg/io/io.go        (Engine.Send entry point)
//   - pkg/io/idalloc.go   (Allocate / Release)
//   - pkg/io/matcher.go   (Register / Cancel / channel for reply)
//   - pkg/io/socketpool.go (PickAt to iterate tuples)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: internal — invoked via Engine.Send. Behaviour is exercised
// by sender_test.go using the in-process echo / black-hole helpers.
package io

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

// runSend implements the retransmit lifecycle described above.
func runSend(ctx context.Context, e *Engine, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error) {
	udst, err := resolveDst(dst)
	if err != nil {
		return nil, fmt.Errorf("io: resolve dst: %w", err)
	}

	maxAttempts := policy.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Allocate a tuple+ID. allocateOnAnyTuple loops over the pool;
		// returns the chosen socket, tuple, and id.
		sock, tuple, id, err := allocateOnAnyTuple(ctx, e, udst)
		if err != nil {
			return nil, err
		}

		// Build the packet for THIS identifier (caller might bake the id
		// into NAS-Port-Id or any other state-bearing attribute).
		req, err := build(id)
		if err != nil {
			e.idAlloc.Release(tuple, id)
			return nil, fmt.Errorf("io: build request (attempt %d): %w", attempt, err)
		}
		if req == nil {
			e.idAlloc.Release(tuple, id)
			return nil, errors.New("io: build returned nil packet")
		}
		// Make sure the identifier on the packet matches what we
		// allocated, regardless of what the build closure did. The
		// matcher keys on this byte.
		req.Identifier = id

		// Encode (also stamps Authenticator for non-Access-Request).
		wire, err := req.Encode()
		if err != nil {
			e.idAlloc.Release(tuple, id)
			return nil, fmt.Errorf("io: encode request (attempt %d): %w", attempt, err)
		}

		localAddr := sock.LocalAddr()

		// Register with the matcher BEFORE writing so an instant reply
		// can't race us.
		replyCh := e.matcher.Register(localAddr, id, req, subID)

		// Write to the wire and capture send time.
		sendT := time.Now()
		_, werr := sock.WriteToUDP(wire, udst)
		if werr != nil {
			e.matcher.Cancel(localAddr, id)
			e.idAlloc.Release(tuple, id)
			// On a write error, give up — the kernel said no.
			e.collector.Submit(events.NewError(
				subID, e.offsetMs(),
				events.EventTypeInternalError, 0,
				"udp write: "+werr.Error(),
			))
			return nil, fmt.Errorf("io: udp write (attempt %d): %w", attempt, werr)
		}

		// Emit request_sent / request_retransmitted.
		e.collector.Submit(events.NewRequestSent(
			subID, e.offsetMs(), "",
			int8(req.Code),
			int16(id),
			localAddr.String(),
			udst.String(),
			int32(len(wire)),
			int32(attempt),
		))

		// Wait for reply / timeout / cancellation.
		timeout := policy.timeoutFor(attempt)
		if timeout <= 0 {
			timeout = policy.InitialTimeout
		}
		timer := time.NewTimer(timeout)

		select {
		case <-ctx.Done():
			timer.Stop()
			e.matcher.Cancel(localAddr, id)
			e.idAlloc.Release(tuple, id)
			return nil, ctx.Err()

		case rd := <-replyCh:
			timer.Stop()
			latencyUs := time.Since(sendT).Microseconds()
			// Matcher already removed itself from pending; release the ID.
			e.idAlloc.Release(tuple, id)

			e.collector.Submit(events.NewReplyReceived(
				subID, e.offsetMs(), "",
				int8(rd.Packet.Code),
				int16(id),
				localAddr.String(),
				rd.RemoteAddr.String(),
				int32(len(rd.WireBytes)),
				latencyUs,
			))

			return &SendResult{
				Reply:       rd.Packet,
				LatencyUs:   latencyUs,
				RetransmitN: attempt,
				LocalAddr:   localAddr,
				Identifier:  id,
			}, nil

		case <-timer.C:
			// Timeout. Cancel the matcher entry and release the ID
			// before allocating a fresh ID for the retransmit.
			e.matcher.Cancel(localAddr, id)
			e.idAlloc.Release(tuple, id)
			// Loop continues: next iteration allocates a new ID.
		}
	}

	// Exhausted retries.
	return nil, ErrFinalTimeout
}

// allocateOnAnyTuple tries Allocate on each socket round-robin until it
// finds one with a free Identifier for the (src, dst) tuple. Backoffs and
// retries up to AllocMaxWait when ALL tuples are full. Returns the chosen
// socket along with the tuple/id so the caller can release it later.
func allocateOnAnyTuple(ctx context.Context, e *Engine, dst *net.UDPAddr) (*net.UDPConn, Tuple, uint8, error) {
	deadline := time.Now().Add(e.opts.AllocMaxWait)
	step := e.opts.AllocBackoffStep
	if step <= 0 {
		step = 5 * time.Millisecond
	}

	n := e.pool.Len()
	startIdx := int(e.pool.rrIdx.Add(1) - 1)

	for {
		for i := 0; i < n; i++ {
			sock := e.pool.PickAt(startIdx + i)
			if sock == nil {
				continue
			}
			localUDP, ok := sock.LocalAddr().(*net.UDPAddr)
			if !ok {
				continue
			}
			tuple := MakeTuple(localUDP, dst)
			if id, ok := e.idAlloc.Allocate(tuple); ok {
				return sock, tuple, id, nil
			}
		}

		// All tuples full — backoff and try again until deadline or ctx.
		if time.Now().After(deadline) {
			return nil, Tuple{}, 0, ErrIdentifierExhausted
		}
		select {
		case <-ctx.Done():
			return nil, Tuple{}, 0, ctx.Err()
		case <-time.After(step):
			// Try again; advance the round-robin start.
			startIdx = int(e.pool.rrIdx.Add(1) - 1)
		}
	}
}
