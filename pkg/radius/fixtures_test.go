// Package radius — fixture-based decode/encode round trip tests.
//
// Purpose:
//
//	Generates a known-shape Access-Request fixture (deterministic, all
//	authenticator bytes preset) into pkg/radius/testdata/, then decodes
//	it and re-encodes it to ensure byte-for-byte stability. Future
//	captured-from-FreeRADIUS fixtures should be added under testdata/.
//
// Related files:
//   - pkg/radius/packet.go
//   - pkg/radius/testdata/*.bin
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixtureAccessRequestDeterministicEncodeDecode(t *testing.T) {
	secret := []byte("testing123") // matches Docker FreeRADIUS rig default

	// Build a deterministic Access-Request: identifier=1, fixed authenticator,
	// User-Name=alice, NAS-IP-Address=10.1.2.3, NAS-Port=42.
	auth := [16]byte{
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}
	p := &Packet{
		Code:          CodeAccessRequest,
		Identifier:    1,
		Authenticator: auth,
		Secret:        secret,
	}
	require.NoError(t, p.Attributes.AddString(AttrUserName, "alice"))
	require.NoError(t, p.Attributes.AddIPv4(AttrNASIPAddress, net.ParseIP("10.1.2.3")))
	require.NoError(t, p.Attributes.AddUint32(AttrNASPort, 42))
	require.NoError(t, p.Attributes.AddUint32(AttrServiceType, ServiceTypeFramed))
	require.NoError(t, p.Attributes.AddUint32(AttrFramedProtocol, FramedProtocolPPP))

	// Encrypt a PAP password manually to keep determinism (NewAccessRequestPAP
	// generates a random authenticator we'd lose control over).
	cipher, err := EncryptUserPassword([]byte("hunter2"), secret, auth)
	require.NoError(t, err)
	require.NoError(t, p.Attributes.Add(AttrUserPassword, cipher))

	wire, err := p.Encode()
	require.NoError(t, err)

	// Persist the fixture for downstream slices to consume / for future
	// FreeRADIUS-rig comparison.
	fixtureDir := filepath.Join("testdata")
	require.NoError(t, os.MkdirAll(fixtureDir, 0o755))
	fixturePath := filepath.Join(fixtureDir, "access-request-pap.bin")
	require.NoError(t, os.WriteFile(fixturePath, wire, 0o644))

	// Decode the fixture from disk and verify all attributes survive.
	loaded, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	got, err := Decode(loaded, secret)
	require.NoError(t, err)
	assert.Equal(t, CodeAccessRequest, got.Code)
	assert.Equal(t, uint8(1), got.Identifier)
	assert.Equal(t, auth, got.Authenticator)
	assert.Equal(t, "alice", got.Attributes.GetString(AttrUserName))

	ip, ok := got.Attributes.GetIPv4(AttrNASIPAddress)
	require.True(t, ok)
	assert.Equal(t, "10.1.2.3", ip.String())

	port, ok := got.Attributes.GetUint32(AttrNASPort)
	require.True(t, ok)
	assert.Equal(t, uint32(42), port)

	st, ok := got.Attributes.GetUint32(AttrServiceType)
	require.True(t, ok)
	assert.Equal(t, ServiceTypeFramed, st)

	upw, ok := got.Attributes.Get(AttrUserPassword)
	require.True(t, ok)
	plain, err := DecryptUserPassword(upw.Value, secret, got.Authenticator)
	require.NoError(t, err)
	assert.Equal(t, "hunter2", string(plain))

	// Re-encode and verify byte equality (deterministic round trip).
	got.Secret = secret
	rewire, err := got.Encode()
	require.NoError(t, err)
	assert.True(t, bytes.Equal(loaded, rewire), "decoded-then-encoded packet must equal original bytes")
}

func TestFixtureAccountingRequestRoundTrip(t *testing.T) {
	secret := []byte("testing123")
	p, err := NewAccountingRequestStart(7, secret,
		Attribute{Type: AttrUserName, Value: []byte("alice")},
		Attribute{Type: AttrAcctSessionID, Value: []byte("0001-aaaa")},
		Attribute{Type: AttrAcctAuthentic, Value: u32(AcctAuthenticRADIUS)},
		Attribute{Type: AttrFramedIPAddress, Value: net.ParseIP("10.10.0.5").To4()},
	)
	require.NoError(t, err)

	wire, err := p.Encode()
	require.NoError(t, err)

	got, err := Decode(wire, secret)
	require.NoError(t, err)
	assert.Equal(t, CodeAccountingRequest, got.Code)
	assert.Equal(t, "alice", got.Attributes.GetString(AttrUserName))

	stype, ok := got.Attributes.GetUint32(AttrAcctStatusType)
	require.True(t, ok)
	assert.Equal(t, AcctStatusStart, stype)

	// Persist for downstream consumers / future FreeRADIUS comparison.
	fixturePath := filepath.Join("testdata", "accounting-request-start.bin")
	require.NoError(t, os.WriteFile(fixturePath, wire, 0o644))
}

// u32 is a tiny helper to inline a 4-byte big-endian integer attribute value
// for use in test fixtures.
func u32(v uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
	return b
}
