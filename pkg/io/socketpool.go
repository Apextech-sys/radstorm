// Package io — UDP socket pool.
//
// Purpose:
//
//	SocketPool binds UDP sockets across the configured source IPs and
//	port range, and hands them out for use by Sender / Receiver. Selection
//	is round-robin to spread load. The pool tracks each socket's
//	(srcIP, srcPort) tuple so callers can build (src, dst) tuples for the
//	identifier allocator.
//
// Related files:
//   - pkg/io/io.go      (Engine constructs the pool)
//   - pkg/io/sender.go  (picks a socket per Send attempt)
//   - pkg/io/receiver.go (one read goroutine per socket)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: internal — SocketPool is exported only for tests; callers
// typically interact via the Engine.
package io

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// SocketPool owns the bound UDP sockets and round-robins selection.
type SocketPool struct {
	sockets []*net.UDPConn
	rrIdx   atomic.Uint64

	closeOnce sync.Once
	closed    atomic.Bool
}

// NewSocketPool binds sockets across srcIPs × [portLo, portHi]. If portLo
// == portHi == 0, one ephemeral-port socket is bound per source IP. A
// best-effort cleanup closes any sockets opened so far on the first
// failure.
func NewSocketPool(srcIPs []net.IP, portLo, portHi int) (*SocketPool, error) {
	if len(srcIPs) == 0 {
		return nil, ErrNoSockets
	}
	if portLo < 0 || portHi < 0 || portLo > 65535 || portHi > 65535 {
		return nil, fmt.Errorf("io: invalid port range [%d,%d]", portLo, portHi)
	}
	if portLo > portHi {
		return nil, fmt.Errorf("io: port range lo (%d) > hi (%d)", portLo, portHi)
	}

	var sockets []*net.UDPConn
	close := func() {
		for _, s := range sockets {
			_ = s.Close()
		}
	}

	for _, ip := range srcIPs {
		if ip == nil {
			close()
			return nil, errors.New("io: nil source IP")
		}
		if portLo == 0 && portHi == 0 {
			c, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: 0})
			if err != nil {
				close()
				return nil, fmt.Errorf("bind %s: ephemeral: %w", ip, err)
			}
			sockets = append(sockets, c)
			continue
		}
		for port := portLo; port <= portHi; port++ {
			c, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: port})
			if err != nil {
				close()
				return nil, fmt.Errorf("bind %s:%d: %w", ip, port, err)
			}
			sockets = append(sockets, c)
		}
	}

	p := &SocketPool{sockets: sockets}
	return p, nil
}

// Sockets returns the underlying *net.UDPConn slice. The receiver uses
// this to launch a per-socket reader goroutine. Callers MUST NOT close
// the returned sockets; use Close on the pool.
func (p *SocketPool) Sockets() []*net.UDPConn {
	return p.sockets
}

// LocalAddrs returns the bound local addresses, one per socket.
func (p *SocketPool) LocalAddrs() []*net.UDPAddr {
	out := make([]*net.UDPAddr, 0, len(p.sockets))
	for _, s := range p.sockets {
		if a, ok := s.LocalAddr().(*net.UDPAddr); ok {
			out = append(out, a)
		}
	}
	return out
}

// Pick returns the next socket round-robin. Safe for concurrent use.
func (p *SocketPool) Pick() *net.UDPConn {
	if len(p.sockets) == 0 {
		return nil
	}
	i := p.rrIdx.Add(1) - 1
	return p.sockets[int(i%uint64(len(p.sockets)))]
}

// PickAt returns the socket at index i (modulo size). Used by Sender to
// iterate through the pool when one tuple's identifier space is exhausted.
func (p *SocketPool) PickAt(i int) *net.UDPConn {
	if len(p.sockets) == 0 {
		return nil
	}
	if i < 0 {
		i = -i
	}
	return p.sockets[i%len(p.sockets)]
}

// Len returns the number of bound sockets.
func (p *SocketPool) Len() int {
	return len(p.sockets)
}

// Close shuts down all sockets. Idempotent.
func (p *SocketPool) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		for _, s := range p.sockets {
			_ = s.Close()
		}
	})
}

// IsClosed reports whether Close has been called.
func (p *SocketPool) IsClosed() bool {
	return p.closed.Load()
}
