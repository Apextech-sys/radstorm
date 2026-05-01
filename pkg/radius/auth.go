// Package radius — authenticator, password, and HMAC math per RFC 2865/2866/2869/5176.
//
// Purpose:
//
//	Implements the cryptographic helpers that turn raw secrets and request
//	bodies into the various authenticator and password ciphertext fields
//	required by the protocol. All MD5 / HMAC-MD5 work for radstorm flows
//	through this file.
//
// Related files:
//   - pkg/radius/packet.go        (calls EncryptUserPassword, ComputeRequestAuthenticator etc.)
//   - pkg/radius/constructors.go  (uses CHAPPassword to build CHAP Access-Requests)
//   - docs/PROTOCOL.md            (PAP / CHAP / Message-Authenticator spec)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: Helpers in this file are exported for use by sibling files only;
// downstream packages should prefer the higher-level constructors. The wire
// formats produced here MUST match RFC byte-for-byte.
package radius

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// AuthenticatorLength is the fixed length of the Authenticator field (16 bytes).
const AuthenticatorLength = 16

// MessageAuthenticatorLength is the fixed length of the Message-Authenticator
// attribute value (16 bytes, HMAC-MD5).
const MessageAuthenticatorLength = 16

// NewRequestAuthenticator returns a 16-byte cryptographically random
// authenticator suitable for an Access-Request.
func NewRequestAuthenticator() ([16]byte, error) {
	var a [16]byte
	if _, err := rand.Read(a[:]); err != nil {
		return a, fmt.Errorf("radius: read random authenticator: %w", err)
	}
	return a, nil
}

// EncryptUserPassword encrypts a User-Password attribute per RFC 2865 §5.2.
//
// Layout: ciphertext = b1 || b2 || ... || bN where each b_i is 16 bytes.
// b1 = p1 XOR MD5(secret || requestAuthenticator)
// bi = pi XOR MD5(secret || c_(i-1))   for i > 1
//
// The cleartext password is null-padded to a multiple of 16 bytes.
// Maximum password length is 128 bytes per RFC 2865 §5.2.
func EncryptUserPassword(password, secret []byte, requestAuthenticator [16]byte) ([]byte, error) {
	if len(password) > 128 {
		return nil, fmt.Errorf("radius: password length %d exceeds 128 bytes", len(password))
	}

	// Pad password to a 16-byte boundary with NUL bytes.
	padded := make([]byte, ((len(password)+15)/16)*16)
	if len(padded) == 0 {
		padded = make([]byte, 16)
	}
	copy(padded, password)

	out := make([]byte, len(padded))
	prev := requestAuthenticator[:]

	for i := 0; i < len(padded); i += 16 {
		h := md5.New()
		h.Write(secret)
		h.Write(prev)
		digest := h.Sum(nil)

		for j := 0; j < 16; j++ {
			out[i+j] = padded[i+j] ^ digest[j]
		}
		prev = out[i : i+16]
	}
	return out, nil
}

// DecryptUserPassword reverses EncryptUserPassword. Returns the cleartext
// with trailing NULs stripped.
func DecryptUserPassword(ciphertext, secret []byte, requestAuthenticator [16]byte) ([]byte, error) {
	if len(ciphertext)%16 != 0 || len(ciphertext) == 0 {
		return nil, fmt.Errorf("radius: ciphertext length %d not a positive multiple of 16", len(ciphertext))
	}

	out := make([]byte, len(ciphertext))
	prev := requestAuthenticator[:]

	for i := 0; i < len(ciphertext); i += 16 {
		h := md5.New()
		h.Write(secret)
		h.Write(prev)
		digest := h.Sum(nil)

		for j := 0; j < 16; j++ {
			out[i+j] = ciphertext[i+j] ^ digest[j]
		}
		prev = ciphertext[i : i+16]
	}

	// Strip trailing NULs (padding).
	for len(out) > 0 && out[len(out)-1] == 0 {
		out = out[:len(out)-1]
	}
	return out, nil
}

// CHAPPassword computes the 17-byte CHAP-Password attribute value per
// RFC 2865 §2.2: chapId || MD5(chapId || password || challenge).
func CHAPPassword(chapID byte, password, challenge []byte) []byte {
	h := md5.New()
	h.Write([]byte{chapID})
	h.Write(password)
	h.Write(challenge)
	digest := h.Sum(nil)

	out := make([]byte, 17)
	out[0] = chapID
	copy(out[1:], digest)
	return out
}

// ComputeRequestAuthenticator computes the Authenticator field for a
// non-Access-Request request packet (Accounting-Request, CoA-Request,
// Disconnect-Request) per RFC 2866 / RFC 5176.
//
//	Authenticator = MD5(Code | Identifier | Length | 16 zero bytes | Attributes | Secret)
//
// The packet header passed in must already have Length set; the
// Authenticator field MUST be zeroed (the caller's responsibility — encode
// flow handles this).
func ComputeRequestAuthenticator(code Code, identifier uint8, length uint16, attributes, secret []byte) [16]byte {
	h := md5.New()
	h.Write([]byte{byte(code), identifier})
	var lengthBytes [2]byte
	binary.BigEndian.PutUint16(lengthBytes[:], length)
	h.Write(lengthBytes[:])
	var zero [16]byte
	h.Write(zero[:])
	h.Write(attributes)
	h.Write(secret)
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ComputeResponseAuthenticator computes the Authenticator for a reply
// (Access-Accept/Reject/Challenge, Accounting-Response, CoA-ACK/NAK,
// Disconnect-ACK/NAK) per RFC 2865 §3:
//
//	Authenticator = MD5(Code | Identifier | Length | RequestAuthenticator | Attributes | Secret)
func ComputeResponseAuthenticator(code Code, identifier uint8, length uint16, requestAuthenticator [16]byte, attributes, secret []byte) [16]byte {
	h := md5.New()
	h.Write([]byte{byte(code), identifier})
	var lengthBytes [2]byte
	binary.BigEndian.PutUint16(lengthBytes[:], length)
	h.Write(lengthBytes[:])
	h.Write(requestAuthenticator[:])
	h.Write(attributes)
	h.Write(secret)
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ComputeMessageAuthenticator returns the HMAC-MD5 over the entire packet
// (with the Message-Authenticator attribute value zeroed during the
// computation) keyed by the shared secret, per RFC 2869 §5.14.
//
// The caller must pass `packetWithZeroedMA`, the exact serialized packet
// bytes with the Message-Authenticator attribute value (not the TL header)
// replaced by 16 zero bytes. Encoding helpers in packet.go handle this.
func ComputeMessageAuthenticator(packetWithZeroedMA, secret []byte) [16]byte {
	mac := hmac.New(md5.New, secret)
	mac.Write(packetWithZeroedMA)
	var out [16]byte
	copy(out[:], mac.Sum(nil))
	return out
}
