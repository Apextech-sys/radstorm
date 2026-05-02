// Package radius — tests for Packet encode/decode and authenticator validation.
//
// Purpose:
//
//	Round-trip Encode/Decode for every packet type radstorm sends, plus
//	validation tests for Message-Authenticator, Response Authenticator,
//	and the assorted error paths (truncation, length mismatch, oversize).
//
// Related files:
//   - pkg/radius/packet.go
//   - pkg/radius/constructors.go
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: build a minimal Access-Request (User-Name only, no password) and round-trip it.
func TestPacketAccessRequestRoundTripPlain(t *testing.T) {
	secret := []byte("s3cr3t")
	p := &Packet{
		Code:       CodeAccessRequest,
		Identifier: 7,
		Secret:     secret,
	}
	require.NoError(t, p.Attributes.AddString(AttrUserName, "alice"))
	require.NoError(t, p.Attributes.AddIPv4(AttrNASIPAddress, net.ParseIP("10.0.0.1")))

	wire, err := p.Encode()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(wire), MinPacketLength)

	got, err := Decode(wire, secret)
	require.NoError(t, err)
	assert.Equal(t, CodeAccessRequest, got.Code)
	assert.Equal(t, uint8(7), got.Identifier)
	assert.Equal(t, p.Authenticator, got.Authenticator)
	assert.Equal(t, "alice", got.Attributes.GetString(AttrUserName))

	ip, ok := got.Attributes.GetIPv4(AttrNASIPAddress)
	require.True(t, ok)
	assert.Equal(t, "10.0.0.1", ip.String())
}

func TestPacketAccessRequestPAPRoundTripDecryptsPassword(t *testing.T) {
	secret := []byte("xyzzy5461")
	p, err := NewAccessRequestPAP(42, secret, "alice", "hunter2")
	require.NoError(t, err)

	wire, err := p.Encode()
	require.NoError(t, err)

	got, err := Decode(wire, secret)
	require.NoError(t, err)
	assert.Equal(t, "alice", got.Attributes.GetString(AttrUserName))

	cipher, ok := got.Attributes.Get(AttrUserPassword)
	require.True(t, ok)
	plain, err := DecryptUserPassword(cipher.Value, secret, got.Authenticator)
	require.NoError(t, err)
	assert.Equal(t, "hunter2", string(plain))
}

func TestPacketAccessRequestCHAPRoundTrip(t *testing.T) {
	secret := []byte("xyzzy5461")
	p, err := NewAccessRequestCHAP(99, secret, "bob", "swordfish")
	require.NoError(t, err)

	wire, err := p.Encode()
	require.NoError(t, err)

	got, err := Decode(wire, secret)
	require.NoError(t, err)

	chapPass, ok := got.Attributes.Get(AttrCHAPPassword)
	require.True(t, ok)
	require.Len(t, chapPass.Value, 17, "CHAP-Password must be 17 bytes")

	chapChal, ok := got.Attributes.Get(AttrCHAPChallenge)
	require.True(t, ok)
	require.Len(t, chapChal.Value, 16, "CHAP-Challenge in this builder is always 16 bytes")

	// Recompute the digest: MD5(chapID || password || challenge) should match.
	want := CHAPPassword(chapPass.Value[0], []byte("swordfish"), chapChal.Value)
	assert.Equal(t, want, chapPass.Value)
}

func TestPacketAccountingRequestStartHasComputedAuthenticator(t *testing.T) {
	secret := []byte("acct-sec")
	p, err := NewAccountingRequestStart(11, secret,
		Attribute{Type: AttrUserName, Value: []byte("alice")},
		Attribute{Type: AttrAcctSessionID, Value: []byte("session-001")},
	)
	require.NoError(t, err)

	wire, err := p.Encode()
	require.NoError(t, err)

	// Verify the on-wire authenticator matches the spec: MD5 over
	// (Code|Identifier|Length|0...|Attrs|Secret).
	totalLen := binary.BigEndian.Uint16(wire[2:4])
	body := wire[20:totalLen]
	want := ComputeRequestAuthenticator(CodeAccountingRequest, 11, totalLen, body, secret)
	assert.Equal(t, want[:], wire[4:20])
}

func TestPacketResponseAuthenticatorComputedAndValidated(t *testing.T) {
	secret := []byte("reply-sec")

	// Build a fake Access-Request to "reply to".
	req, err := NewAccessRequestPAP(13, secret, "carol", "pw")
	require.NoError(t, err)
	_, err = req.Encode()
	require.NoError(t, err)

	// Build the matching Access-Accept reply.
	reply := &Packet{
		Code:                 CodeAccessAccept,
		Identifier:           req.Identifier,
		RequestAuthenticator: req.Authenticator,
		Secret:               secret,
	}
	require.NoError(t, reply.Attributes.AddString(AttrReplyMessage, "welcome"))

	wire, err := reply.Encode()
	require.NoError(t, err)

	got, err := Decode(wire, secret)
	require.NoError(t, err)
	assert.True(t, got.ValidateResponseAuthenticator(req, secret, wire), "valid reply must pass authenticator check")

	// Tamper one byte and verify failure.
	tampered := append([]byte(nil), wire...)
	tampered[len(tampered)-1] ^= 0xff
	gotTampered, err := Decode(tampered, secret)
	require.NoError(t, err) // structural decode still works
	assert.False(t, gotTampered.ValidateResponseAuthenticator(req, secret, tampered),
		"tampered reply must fail authenticator check")
}

func TestPacketMessageAuthenticatorEncodeAndValidate(t *testing.T) {
	secret := []byte("ma-sec")

	// Simulate an inbound CoA-Request and then build a CoA-ACK; the ACK
	// is the easiest way to exercise the encode-side MA computation
	// (NewCoAAck installs the placeholder).
	req := &Packet{
		Code:       CodeCoARequest,
		Identifier: 33,
		Secret:     secret,
	}
	require.NoError(t, req.Attributes.AddString(AttrUserName, "alice"))
	// fake a request authenticator that the server would have set
	for i := range req.Authenticator {
		req.Authenticator[i] = byte(i + 1)
	}

	ack, err := NewCoAAck(req, secret)
	require.NoError(t, err)
	wire, err := ack.Encode()
	require.NoError(t, err)

	// Decode and verify Message-Authenticator validates.
	got, err := Decode(wire, secret)
	require.NoError(t, err)
	assert.True(t, got.ValidateMessageAuthenticator(wire, secret), "valid MA must pass")

	// Tamper the User-Name byte (not the MA itself) and verify validation fails.
	tampered := append([]byte(nil), wire...)
	tampered[20] ^= 0xff
	gotT, err := Decode(tampered, secret)
	require.NoError(t, err)
	assert.False(t, gotT.ValidateMessageAuthenticator(tampered, secret),
		"tampered packet must fail MA validation")

	// Tamper the MA value itself.
	tampered2 := append([]byte(nil), wire...)
	tampered2[len(tampered2)-1] ^= 0xff
	gotT2, err := Decode(tampered2, secret)
	require.NoError(t, err)
	assert.False(t, gotT2.ValidateMessageAuthenticator(tampered2, secret),
		"tampered MA value must fail validation")
}

func TestPacketDecodeErrors(t *testing.T) {
	t.Run("too short", func(t *testing.T) {
		_, err := Decode(make([]byte, 10), nil)
		assert.ErrorIs(t, err, ErrPacketTooShort)
	})
	t.Run("too long", func(t *testing.T) {
		_, err := Decode(make([]byte, MaxPacketLength+1), nil)
		assert.ErrorIs(t, err, ErrPacketTooLong)
	})
	t.Run("length mismatch", func(t *testing.T) {
		buf := make([]byte, 25)
		buf[0] = byte(CodeAccessRequest)
		buf[1] = 1
		// Declared length 100 but only 25 bytes provided.
		binary.BigEndian.PutUint16(buf[2:4], 100)
		_, err := Decode(buf, nil)
		assert.ErrorIs(t, err, ErrLengthMismatch)
	})
}

func TestPacketEncodeRequiresSecretForNonAccessRequest(t *testing.T) {
	p := &Packet{Code: CodeAccountingRequest, Identifier: 1}
	_, err := p.Encode()
	require.Error(t, err)
}

func TestPacketEncodeOversize(t *testing.T) {
	secret := []byte("s")
	p := &Packet{
		Code:       CodeAccessRequest,
		Identifier: 1,
		Secret:     secret,
	}
	// Pack ~20 maximum-length attributes (each 255 bytes encoded) to exceed 4096.
	val := make([]byte, MaxAttributeValueLength)
	for i := 0; i < 20; i++ {
		require.NoError(t, p.Attributes.Add(AttrUserName, val))
	}
	_, err := p.Encode()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestConstructorsRejectCredentialAttrInExtras(t *testing.T) {
	secret := []byte("s")
	_, err := NewAccessRequestPAP(1, secret, "u", "p",
		Attribute{Type: AttrCHAPPassword, Value: []byte("nope")},
	)
	assert.Error(t, err)

	_, err = NewAccessRequestCHAP(1, secret, "u", "p",
		Attribute{Type: AttrUserPassword, Value: []byte("nope")},
	)
	assert.Error(t, err)
}

func TestConstructorsCoANakDisconnectNakSetErrorCause(t *testing.T) {
	secret := []byte("s")
	req := &Packet{Code: CodeCoARequest, Identifier: 5, Secret: secret}
	for i := range req.Authenticator {
		req.Authenticator[i] = byte(i)
	}
	nak, err := NewCoANak(req, secret, ErrorCauseSessionContextNotFound)
	require.NoError(t, err)
	v, ok := nak.Attributes.GetUint32(AttrErrorCause)
	require.True(t, ok)
	assert.Equal(t, ErrorCauseSessionContextNotFound, v)

	dreq := &Packet{Code: CodeDisconnectRequest, Identifier: 6, Secret: secret}
	dnak, err := NewDisconnectNak(dreq, secret, ErrorCauseNASIdentificationMismatch)
	require.NoError(t, err)
	v2, ok := dnak.Attributes.GetUint32(AttrErrorCause)
	require.True(t, ok)
	assert.Equal(t, ErrorCauseNASIdentificationMismatch, v2)
}

func TestConstructorsAckRejectsNilRequest(t *testing.T) {
	secret := []byte("s")
	_, err := NewCoAAck(nil, secret)
	assert.Error(t, err)
	_, err = NewCoANak(nil, secret, 0)
	assert.Error(t, err)
	_, err = NewDisconnectAck(nil, secret)
	assert.Error(t, err)
	_, err = NewDisconnectNak(nil, secret, 0)
	assert.Error(t, err)
}

// TestPacketRequestAuthenticatorPreservedOnEncode ensures the same
// encoded wire is produced if Authenticator is pre-set (deterministic
// for testing flows that need to capture exact bytes).
func TestPacketRequestAuthenticatorPreservedOnEncode(t *testing.T) {
	secret := []byte("s")
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	p := &Packet{
		Code:          CodeAccessRequest,
		Identifier:    1,
		Authenticator: auth,
		Secret:        secret,
	}
	require.NoError(t, p.Attributes.AddString(AttrUserName, "x"))

	wire1, err := p.Encode()
	require.NoError(t, err)
	wire2, err := p.Encode()
	require.NoError(t, err)
	assert.True(t, bytes.Equal(wire1, wire2), "deterministic encode with preset authenticator")
	assert.Equal(t, auth, p.Authenticator)
}
