// Package radius — tests for authenticator/PAP/CHAP/HMAC math.
//
// Purpose:
//
//	Validates RFC 2865 §5.2 PAP encryption, RFC 2865 §2.2 CHAP-Password,
//	RFC 2866 §3 Accounting-Request authenticator, RFC 2865 §3 reply
//	authenticator, and RFC 2869 §5.14 Message-Authenticator computations
//	against vectors derived from the algorithm specifications.
//
// Related files:
//   - pkg/radius/auth.go
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptUserPasswordRoundTrip(t *testing.T) {
	secret := []byte("xyzzy5461")
	auth := [16]byte{0xc0, 0xfa, 0xa3, 0x67, 0xc6, 0xf3, 0x9b, 0x9c,
		0x44, 0x33, 0xee, 0x88, 0x07, 0x77, 0x12, 0x34}

	cases := []string{
		"",                           // empty → still pads to 16
		"hi",                         // shorter than 16
		"sixteen-byte-pwd",           // exactly 16
		"this is a longer password!", // > 16
		"abcdefghijklmnopqrstuvwxyz0123456789", // > 32
	}
	for _, pw := range cases {
		t.Run(pw, func(t *testing.T) {
			cipher, err := EncryptUserPassword([]byte(pw), secret, auth)
			require.NoError(t, err)
			assert.Equal(t, 0, len(cipher)%16)
			assert.GreaterOrEqual(t, len(cipher), 16)

			plain, err := DecryptUserPassword(cipher, secret, auth)
			require.NoError(t, err)
			assert.Equal(t, pw, string(plain))
		})
	}
}

func TestEncryptUserPasswordKnownVector(t *testing.T) {
	// Reference computation following RFC 2865 §5.2 directly.
	// For a 1-block password: cipher = pw_padded XOR MD5(secret || RA).
	secret := []byte("secret")
	auth := [16]byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	}
	password := "password"

	h := md5.New()
	h.Write(secret)
	h.Write(auth[:])
	digest := h.Sum(nil)

	want := make([]byte, 16)
	padded := make([]byte, 16)
	copy(padded, password)
	for i := 0; i < 16; i++ {
		want[i] = padded[i] ^ digest[i]
	}

	got, err := EncryptUserPassword([]byte(password), secret, auth)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestEncryptUserPasswordOversize(t *testing.T) {
	secret := []byte("x")
	auth := [16]byte{}
	tooLong := make([]byte, 129)
	_, err := EncryptUserPassword(tooLong, secret, auth)
	require.Error(t, err)
}

func TestDecryptUserPasswordRejectsBadLength(t *testing.T) {
	secret := []byte("x")
	auth := [16]byte{}
	_, err := DecryptUserPassword([]byte{1, 2, 3}, secret, auth)
	require.Error(t, err)

	_, err = DecryptUserPassword(nil, secret, auth)
	require.Error(t, err)
}

func TestCHAPPasswordKnownVector(t *testing.T) {
	// RFC 2865 §2.2 reference: result = chapID || MD5(chapID || password || challenge).
	chapID := byte(0x42)
	password := []byte("hunter2")
	challenge := []byte{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22,
		0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00,
	}

	h := md5.New()
	h.Write([]byte{chapID})
	h.Write(password)
	h.Write(challenge)
	digest := h.Sum(nil)

	want := append([]byte{chapID}, digest...)
	got := CHAPPassword(chapID, password, challenge)
	assert.Equal(t, want, got)
	assert.Len(t, got, 17, "CHAP-Password is 1-byte ID + 16-byte digest")
}

func TestComputeRequestAuthenticatorMatchesSpec(t *testing.T) {
	// MD5(Code|Identifier|Length|16 zero|Attributes|Secret) per RFC 2866 §3.
	secret := []byte("topsecret")
	attrs := []byte{1, 7, 'a', 'l', 'i', 'c', 'e'} // User-Name = "alice"
	const length uint16 = 27                       // 20 header + 7 attr

	h := md5.New()
	h.Write([]byte{byte(CodeAccountingRequest), 0x05})
	var lb [2]byte
	binary.BigEndian.PutUint16(lb[:], length)
	h.Write(lb[:])
	var zero [16]byte
	h.Write(zero[:])
	h.Write(attrs)
	h.Write(secret)
	want := h.Sum(nil)

	got := ComputeRequestAuthenticator(CodeAccountingRequest, 5, length, attrs, secret)
	assert.Equal(t, want, got[:])
}

func TestComputeResponseAuthenticatorMatchesSpec(t *testing.T) {
	// MD5(Code|Identifier|Length|RequestAuthenticator|Attributes|Secret).
	secret := []byte("rad-secret")
	requestAuth := [16]byte{
		0xde, 0xad, 0xbe, 0xef, 0xfe, 0xed, 0xfa, 0xce,
		0xca, 0xfe, 0xba, 0xbe, 0x12, 0x34, 0x56, 0x78,
	}
	attrs := []byte{} // empty Access-Accept
	const length uint16 = 20

	h := md5.New()
	h.Write([]byte{byte(CodeAccessAccept), 0x09})
	var lb [2]byte
	binary.BigEndian.PutUint16(lb[:], length)
	h.Write(lb[:])
	h.Write(requestAuth[:])
	h.Write(attrs)
	h.Write(secret)
	want := h.Sum(nil)

	got := ComputeResponseAuthenticator(CodeAccessAccept, 9, length, requestAuth, attrs, secret)
	assert.Equal(t, want, got[:])
}

func TestComputeMessageAuthenticatorMatchesHMAC(t *testing.T) {
	secret := []byte("ma-secret")
	packet := []byte{
		// header
		byte(CodeCoARequest), 0x10, 0x00, 0x26, // length 38
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // request authenticator
		// attribute body (User-Name = "x", then Message-Authenticator with zero value)
		1, 3, 'x',
		80, 18, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}

	mac := hmac.New(md5.New, secret)
	mac.Write(packet)
	want := mac.Sum(nil)

	got := ComputeMessageAuthenticator(packet, secret)
	assert.Equal(t, want, got[:])
}

func TestNewRequestAuthenticatorIsRandom(t *testing.T) {
	a, err := NewRequestAuthenticator()
	require.NoError(t, err)
	b, err := NewRequestAuthenticator()
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "two consecutive authenticators must differ (probabilistically certain)")
}
