// Package io — IDAllocator tests.
//
// Purpose:
//
//	Verifies the per-tuple bitmap allocator: exhaustion, release,
//	isolation between tuples, and concurrent allocate/release safety
//	(run with -race).
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tupleFor(srcIP string, srcPort int, dstIP string, dstPort int) Tuple {
	src := &net.UDPAddr{IP: net.ParseIP(srcIP), Port: srcPort}
	dst := &net.UDPAddr{IP: net.ParseIP(dstIP), Port: dstPort}
	return MakeTuple(src, dst)
}

func TestIDAllocator_AllocateAndReleaseSimple(t *testing.T) {
	a := NewIDAllocator()
	tup := tupleFor("127.0.0.1", 5000, "127.0.0.1", 1812)

	id1, ok := a.Allocate(tup)
	require.True(t, ok)
	id2, ok := a.Allocate(tup)
	require.True(t, ok)
	require.NotEqual(t, id1, id2, "allocator must not hand out the same id twice")

	require.Equal(t, 2, a.Inflight(tup))

	a.Release(tup, id1)
	require.Equal(t, 1, a.Inflight(tup))
	a.Release(tup, id2)
	require.Equal(t, 0, a.Inflight(tup))
}

func TestIDAllocator_ExhaustsAt256(t *testing.T) {
	a := NewIDAllocator()
	tup := tupleFor("127.0.0.1", 5001, "127.0.0.1", 1812)

	seen := make(map[uint8]bool, 256)
	for i := 0; i < 256; i++ {
		id, ok := a.Allocate(tup)
		require.Truef(t, ok, "allocation %d should succeed", i)
		require.Falsef(t, seen[id], "id %d handed out twice (iteration %d)", id, i)
		seen[id] = true
	}
	require.Len(t, seen, 256)

	// 257th must fail.
	_, ok := a.Allocate(tup)
	require.False(t, ok, "257th allocation must fail")
	require.Equal(t, 256, a.Inflight(tup))

	// Release one and allocate again.
	a.Release(tup, 42)
	require.Equal(t, 255, a.Inflight(tup))
	id, ok := a.Allocate(tup)
	require.True(t, ok)
	require.Equal(t, uint8(42), id, "released id should be reusable")
}

func TestIDAllocator_TuplesAreIndependent(t *testing.T) {
	a := NewIDAllocator()
	tup1 := tupleFor("127.0.0.1", 5002, "127.0.0.1", 1812)
	tup2 := tupleFor("127.0.0.1", 5003, "127.0.0.1", 1812)

	// Exhaust tup1.
	for i := 0; i < 256; i++ {
		_, ok := a.Allocate(tup1)
		require.True(t, ok)
	}
	_, ok := a.Allocate(tup1)
	require.False(t, ok)

	// tup2 should still be wide open.
	_, ok = a.Allocate(tup2)
	require.True(t, ok)
	require.Equal(t, 1, a.Inflight(tup2))
	require.Equal(t, 256, a.Inflight(tup1))
}

func TestIDAllocator_ConcurrentNoDoubleAlloc(t *testing.T) {
	a := NewIDAllocator()
	tup := tupleFor("127.0.0.1", 5004, "127.0.0.1", 1812)

	const workers = 64
	const opsPerWorker = 200

	var wg sync.WaitGroup
	doubleAlloc := atomic.Int64{}

	// Use a per-id counter to detect any time two goroutines hold the
	// same id at the same time.
	var holders [256]atomic.Int32

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				id, ok := a.Allocate(tup)
				if !ok {
					continue
				}
				if holders[id].Add(1) != 1 {
					doubleAlloc.Add(1)
				}
				// brief hold
				holders[id].Add(-1)
				a.Release(tup, id)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(0), doubleAlloc.Load(),
		"observed double-allocation under concurrent load")
	// All held ids should be released.
	require.Equal(t, 0, a.Inflight(tup))
}

func TestIDAllocator_ReleaseUnknownIsNoop(t *testing.T) {
	a := NewIDAllocator()
	tup := tupleFor("127.0.0.1", 5005, "127.0.0.1", 1812)

	// Release on a fresh tuple should not panic.
	a.Release(tup, 7)
	assert.Equal(t, 0, a.Inflight(tup))

	id, ok := a.Allocate(tup)
	require.True(t, ok)
	// Double release.
	a.Release(tup, id)
	a.Release(tup, id)
	assert.Equal(t, 0, a.Inflight(tup))
}

func TestIDAllocator_TupleCount(t *testing.T) {
	a := NewIDAllocator()
	require.Equal(t, 0, a.TupleCount())
	t1 := tupleFor("127.0.0.1", 6000, "127.0.0.1", 1812)
	t2 := tupleFor("127.0.0.1", 6001, "127.0.0.1", 1812)
	_, _ = a.Allocate(t1)
	require.Equal(t, 1, a.TupleCount())
	_, _ = a.Allocate(t2)
	require.Equal(t, 2, a.TupleCount())
	// Same tuple shouldn't increase count.
	_, _ = a.Allocate(t1)
	require.Equal(t, 2, a.TupleCount())
}

func TestMakeTuple_NormalisesIPv4(t *testing.T) {
	src := &net.UDPAddr{IP: net.ParseIP("127.0.0.1").To4(), Port: 5000}
	dst := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1812}
	tup1 := MakeTuple(src, dst)

	src2 := &net.UDPAddr{IP: net.ParseIP("127.0.0.1").To16(), Port: 5000}
	dst2 := &net.UDPAddr{IP: net.ParseIP("127.0.0.1").To4(), Port: 1812}
	tup2 := MakeTuple(src2, dst2)

	require.Equal(t, tup1, tup2, "v4 and v4-mapped v6 should hash to same tuple")
}
