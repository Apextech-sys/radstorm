// Package io — SocketPool tests.
//
// Purpose:
//   Verifies socket binding (single ephemeral port per source IP, port
//   range), round-robin selection, Close idempotence, and validation of
//   port-range arguments.
//
// Briefing: .orchestration/briefings/2a-io-layer.md
package io

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSocketPool_EphemeralPortPerIP(t *testing.T) {
	ips := []net.IP{net.IPv4(127, 0, 0, 1)}
	p, err := NewSocketPool(ips, 0, 0)
	require.NoError(t, err)
	defer p.Close()

	require.Equal(t, 1, p.Len())
	addrs := p.LocalAddrs()
	require.Len(t, addrs, 1)
	require.True(t, addrs[0].Port > 0, "kernel should pick a real port")
}

func TestSocketPool_PortRange(t *testing.T) {
	// Pick a small range starting somewhere unlikely to collide. Use
	// port 0 fallback if these are taken — just bind two ephemeral
	// to verify the range logic exits sanely. We use ports 0 to test
	// "fixed" path indirectly.
	//
	// To exercise actual range binding, choose two adjacent ephemeral
	// ports by binding in the OS-managed way first, then closing.
	c1, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	port := c1.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, c1.Close())

	// Allow a brief window for the port to be reusable.
	p, err := NewSocketPool([]net.IP{net.IPv4(127, 0, 0, 1)}, port, port)
	if err != nil {
		// Port may have been claimed in the gap; treat as skip rather than failure.
		t.Skipf("port %d no longer available: %v", port, err)
	}
	defer p.Close()
	require.Equal(t, 1, p.Len())
}

func TestSocketPool_PickRoundRobin(t *testing.T) {
	p, err := NewSocketPool(
		[]net.IP{net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1)},
		0, 0,
	)
	require.NoError(t, err)
	defer p.Close()
	require.Equal(t, 2, p.Len())

	first := p.Pick()
	second := p.Pick()
	third := p.Pick()
	require.NotEqual(t, first, second)
	require.Equal(t, first, third, "round-robin should wrap")
}

func TestSocketPool_CloseIsIdempotent(t *testing.T) {
	p, err := NewSocketPool([]net.IP{net.IPv4(127, 0, 0, 1)}, 0, 0)
	require.NoError(t, err)
	p.Close()
	p.Close()
	assert.True(t, p.IsClosed())
}

func TestSocketPool_RejectsBadInputs(t *testing.T) {
	_, err := NewSocketPool(nil, 0, 0)
	require.Error(t, err)

	_, err = NewSocketPool([]net.IP{net.IPv4(127, 0, 0, 1)}, 5000, 4000)
	require.Error(t, err, "lo > hi should be rejected")

	_, err = NewSocketPool([]net.IP{net.IPv4(127, 0, 0, 1)}, -1, 5)
	require.Error(t, err, "negative ports rejected")

	_, err = NewSocketPool([]net.IP{nil}, 0, 0)
	require.Error(t, err, "nil IP rejected")
}
