// Package radius — tests for AttributeList and wire encode/decode helpers.
//
// Purpose:
//
//	Round-trip test for the attribute encode/decode helpers and coverage
//	of the typed Get* accessors. Also exercises edge cases (oversize
//	values, truncated buffers, repeated attributes).
//
// Related files:
//   - pkg/radius/attribute.go
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttributeAddAndAccessors(t *testing.T) {
	var list AttributeList
	require.NoError(t, list.AddString(AttrUserName, "alice@example.com"))
	require.NoError(t, list.AddIPv4(AttrNASIPAddress, net.ParseIP("10.0.0.1")))
	require.NoError(t, list.AddUint32(AttrServiceType, ServiceTypeFramed))
	require.NoError(t, list.AddUint32(AttrFramedProtocol, FramedProtocolPPP))
	require.NoError(t, list.AddString(AttrCallingStationID, "AA-BB-CC-DD-EE-FF"))

	assert.Equal(t, "alice@example.com", list.GetString(AttrUserName))

	ip, ok := list.GetIPv4(AttrNASIPAddress)
	require.True(t, ok)
	assert.Equal(t, "10.0.0.1", ip.String())

	st, ok := list.GetUint32(AttrServiceType)
	require.True(t, ok)
	assert.Equal(t, ServiceTypeFramed, st)

	_, ok = list.Get(AttrAcctSessionID)
	assert.False(t, ok, "missing attribute should report not present")
}

func TestAttributeOversizeRejected(t *testing.T) {
	var list AttributeList
	tooBig := make([]byte, MaxAttributeValueLength+1)
	err := list.Add(AttrUserName, tooBig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestAttributeRoundTrip(t *testing.T) {
	var list AttributeList
	require.NoError(t, list.AddString(AttrUserName, "bob"))
	require.NoError(t, list.AddUint32(AttrNASPort, 1234))
	require.NoError(t, list.AddIPv4(AttrFramedIPAddress, net.ParseIP("192.168.50.7")))

	encoded, err := encodeAttributes(list)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	decoded, err := decodeAttributes(encoded)
	require.NoError(t, err)
	require.Len(t, decoded, 3)

	assert.Equal(t, AttrUserName, decoded[0].Type)
	assert.Equal(t, "bob", string(decoded[0].Value))

	port, ok := decoded.GetUint32(AttrNASPort)
	require.True(t, ok)
	assert.Equal(t, uint32(1234), port)

	ip, ok := decoded.GetIPv4(AttrFramedIPAddress)
	require.True(t, ok)
	assert.Equal(t, "192.168.50.7", ip.String())
}

func TestAttributeDecodeTruncated(t *testing.T) {
	// Type=1 (User-Name), Length=10, but only 4 bytes follow.
	bad := []byte{1, 10, 'x', 'y', 'z', 'w'}
	_, err := decodeAttributes(bad)
	assert.ErrorIs(t, err, ErrTruncatedAttribute)

	// Single trailing byte (no length).
	_, err = decodeAttributes([]byte{1})
	assert.ErrorIs(t, err, ErrTruncatedAttribute)

	// Length byte less than 2 (header itself is 2).
	_, err = decodeAttributes([]byte{1, 1})
	assert.ErrorIs(t, err, ErrTruncatedAttribute)
}

func TestAttributeRepeatedAndRemove(t *testing.T) {
	var list AttributeList
	require.NoError(t, list.AddString(AttrReplyMessage, "hello"))
	require.NoError(t, list.AddString(AttrReplyMessage, "world"))
	require.NoError(t, list.AddString(AttrUserName, "carol"))

	all := list.GetAll(AttrReplyMessage)
	require.Len(t, all, 2)
	assert.Equal(t, "hello", string(all[0].Value))
	assert.Equal(t, "world", string(all[1].Value))

	removed := list.Remove(AttrReplyMessage)
	assert.Equal(t, 2, removed)
	assert.Empty(t, list.GetAll(AttrReplyMessage))
	assert.Equal(t, "carol", list.GetString(AttrUserName))
}

func TestAttributeAddIPv4Rejectsv6(t *testing.T) {
	var list AttributeList
	err := list.AddIPv4(AttrNASIPAddress, net.ParseIP("2001:db8::1"))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not a valid IPv4"))
}
