// Package io — per-tuple 8-bit RADIUS Identifier allocator.
//
// Purpose:
//
//	The RADIUS Identifier is 8 bits, so at most 256 in-flight requests
//	can exist for any given (srcIP, srcPort, dstIP, dstPort) tuple. The
//	allocator maintains a 256-bit bitmap per tuple and serves
//	non-blocking Allocate / Release. Thread-safe.
//
// Related files:
//   - pkg/io/io.go     (Engine constructs the allocator)
//   - pkg/io/sender.go (allocates a fresh ID per transmission attempt)
//   - docs/PROTOCOL.md (Identifier rules, retransmit semantics)
//
// Briefing: .orchestration/briefings/2a-io-layer.md
//
// Contract: internal — but Tuple is referenced by reply matcher and
// tests; both live in this package.
package io

import (
	"net"
	"sync"
)

// Tuple is the 4-tuple that scopes a RADIUS Identifier. Two requests
// MUST NOT share an Identifier within the same Tuple while either is
// in flight.
//
// Stored as fixed-byte fields rather than net.IP slices so the type is
// comparable and usable as a map key without an extra hash step.
type Tuple struct {
	SrcIP   [16]byte
	SrcPort uint16
	DstIP   [16]byte
	DstPort uint16
}

// MakeTuple builds a Tuple from src/dst UDP addresses. IPv4 addresses
// are stored in their IPv4-mapped IPv6 form so v4 / v6 callers using
// the same address pick the same bucket.
func MakeTuple(src, dst *net.UDPAddr) Tuple {
	var t Tuple
	copyIPInto(&t.SrcIP, src.IP)
	copyIPInto(&t.DstIP, dst.IP)
	t.SrcPort = uint16(src.Port)
	t.DstPort = uint16(dst.Port)
	return t
}

func copyIPInto(dst *[16]byte, ip net.IP) {
	if v4 := ip.To4(); v4 != nil {
		// Store as IPv4-mapped IPv6.
		dst[10] = 0xff
		dst[11] = 0xff
		copy(dst[12:], v4)
		return
	}
	if v6 := ip.To16(); v6 != nil {
		copy(dst[:], v6)
	}
}

// idBitmap tracks 256 IDs as 4×uint64 bits + a count.
type idBitmap struct {
	mu    sync.Mutex
	bits  [4]uint64
	count int   // number of allocated IDs
	next  uint8 // hint for next scan start to spread allocations
}

// allocate finds and returns an unset bit, or (0, false) if all set.
// The returned uint8 is the RADIUS Identifier.
func (b *idBitmap) allocate() (uint8, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 256 {
		return 0, false
	}
	// Walk all 256 ids starting from b.next so we don't always reuse
	// the lowest free slot — that's better for late-arrival duplicate
	// detection (gives stale replies more time to drain before the ID
	// recycles).
	for i := 0; i < 256; i++ {
		id := uint8((int(b.next) + i) % 256)
		w := id / 64
		bit := uint64(1) << (id % 64)
		if b.bits[w]&bit == 0 {
			b.bits[w] |= bit
			b.count++
			b.next = id + 1
			return id, true
		}
	}
	return 0, false
}

// release clears the bit for id. No-op if already clear.
func (b *idBitmap) release(id uint8) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := id / 64
	bit := uint64(1) << (id % 64)
	if b.bits[w]&bit != 0 {
		b.bits[w] &^= bit
		b.count--
	}
}

// inflight reports the number of currently-allocated IDs.
func (b *idBitmap) inflight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// IDAllocator hands out RADIUS Identifiers per (src, dst) tuple.
type IDAllocator struct {
	mu       sync.Mutex
	perTuple map[Tuple]*idBitmap
}

// NewIDAllocator returns a fresh allocator with no tuples registered.
// Buckets are created lazily on first Allocate.
func NewIDAllocator() *IDAllocator {
	return &IDAllocator{perTuple: make(map[Tuple]*idBitmap)}
}

// Allocate returns a free Identifier for the tuple, or (0, false) if all
// 256 IDs are in flight. Non-blocking. Safe for concurrent use.
func (a *IDAllocator) Allocate(t Tuple) (uint8, bool) {
	a.mu.Lock()
	bm, ok := a.perTuple[t]
	if !ok {
		bm = &idBitmap{}
		a.perTuple[t] = bm
	}
	a.mu.Unlock()
	return bm.allocate()
}

// Release returns id to the free pool for the tuple. Safe to call on a
// tuple/id that was never allocated (no-op).
func (a *IDAllocator) Release(t Tuple, id uint8) {
	a.mu.Lock()
	bm, ok := a.perTuple[t]
	a.mu.Unlock()
	if !ok {
		return
	}
	bm.release(id)
}

// Inflight reports how many IDs are currently allocated for the tuple.
func (a *IDAllocator) Inflight(t Tuple) int {
	a.mu.Lock()
	bm, ok := a.perTuple[t]
	a.mu.Unlock()
	if !ok {
		return 0
	}
	return bm.inflight()
}

// TupleCount returns the number of distinct tuples that have allocated
// at least one ID.
func (a *IDAllocator) TupleCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.perTuple)
}
