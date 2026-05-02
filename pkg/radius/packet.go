// Package radius — Packet struct, wire encoding, decoding, and validation.
//
// Purpose:
//
//	Defines the Packet wrapper consumed everywhere in radstorm and
//	implements the byte-for-byte encode/decode of a RADIUS datagram
//	per RFC 2865 §3. Also exposes ValidateMessageAuthenticator and
//	ValidateResponseAuthenticator used by inbound paths.
//
// Related files:
//   - pkg/radius/auth.go          (authenticator + Message-Authenticator math)
//   - pkg/radius/attribute.go     (attribute encode/decode helpers)
//   - pkg/radius/constructors.go  (high-level packet builders)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: Packet, Encode, Decode and the validation methods are the
// primary public surface of pkg/radius. pkg/io and pkg/server depend on
// these signatures.
package radius

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MinPacketLength is the smallest legal RADIUS packet (header only).
const MinPacketLength = 20

// MaxPacketLength is the maximum legal RADIUS packet length per RFC 2865 §3.
const MaxPacketLength = 4096

// Packet is the in-memory representation of a RADIUS datagram.
//
// Authenticator semantics depend on Code:
//   - Access-Request: random 16 bytes, set on Encode if zeroed.
//   - Accounting/CoA/Disconnect Request: computed during Encode.
//   - Replies (Access-Accept/Reject, Accounting-Response, CoA/Disconnect ACK/NAK):
//     computed during Encode using RequestAuthenticator.
//
// Secret is never serialized; it stays in memory only for the duration of
// encode/decode/validation calls.
type Packet struct {
	Code          Code
	Identifier    uint8
	Authenticator [16]byte
	Attributes    AttributeList

	// RequestAuthenticator is used only when Encode-ing a response packet;
	// it is the Authenticator field of the request being answered. Ignored
	// for request packets.
	RequestAuthenticator [16]byte

	// Secret is the shared secret. Required by Encode/Decode but never put
	// on the wire. Kept on the struct as a convenience because every method
	// needs it; callers can also pass it explicitly via the *WithSecret APIs.
	Secret []byte
}

// Errors returned by Decode.
var (
	ErrPacketTooShort           = errors.New("radius: packet shorter than 20 bytes")
	ErrPacketTooLong            = errors.New("radius: packet exceeds 4096 bytes")
	ErrLengthMismatch           = errors.New("radius: header Length disagrees with datagram size")
	ErrUnknownCode              = errors.New("radius: unknown packet code")
	ErrInvalidMessageAuthLength = errors.New("radius: Message-Authenticator attribute is not 16 bytes")
)

// Encode serializes the packet to wire bytes, computing Authenticator and
// any Message-Authenticator attribute value as required for the Code.
//
// For Access-Request: if Authenticator is the zero value, a fresh random
// authenticator is generated. The provided value is otherwise used as-is.
//
// For request packets that require a computed authenticator
// (Accounting/CoA/Disconnect Request), Authenticator is overwritten.
//
// For response packets, Authenticator is computed from RequestAuthenticator.
//
// If the Attributes contain a Message-Authenticator (attr 80), its value
// is recomputed as HMAC-MD5 over the whole packet (with that field
// zeroed during computation).
func (p *Packet) Encode() ([]byte, error) {
	if len(p.Secret) == 0 && p.Code != CodeAccessRequest {
		// Access-Request can technically encode without secret if no
		// User-Password and no Message-Authenticator are present; other
		// codes always need it for the authenticator math.
		return nil, errors.New("radius: shared secret required for encode")
	}

	// Pre-encode the attribute body so we know length and can do the
	// authenticator math against the body.
	attrsBody, err := encodeAttributes(p.Attributes)
	if err != nil {
		return nil, fmt.Errorf("encode attributes: %w", err)
	}

	totalLen := MinPacketLength + len(attrsBody)
	if totalLen > MaxPacketLength {
		return nil, fmt.Errorf("radius: encoded packet length %d exceeds %d", totalLen, MaxPacketLength)
	}

	switch {
	case p.Code == CodeAccessRequest:
		if isZeroAuth(p.Authenticator) {
			a, err := NewRequestAuthenticator()
			if err != nil {
				return nil, err
			}
			p.Authenticator = a
		}
	case p.Code == CodeAccountingRequest || p.Code == CodeCoARequest || p.Code == CodeDisconnectRequest:
		p.Authenticator = ComputeRequestAuthenticator(
			p.Code, p.Identifier, uint16(totalLen), attrsBody, p.Secret,
		)
	case p.Code.IsResponse():
		p.Authenticator = ComputeResponseAuthenticator(
			p.Code, p.Identifier, uint16(totalLen), p.RequestAuthenticator, attrsBody, p.Secret,
		)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownCode, p.Code)
	}

	out := make([]byte, totalLen)
	out[0] = byte(p.Code)
	out[1] = p.Identifier
	binary.BigEndian.PutUint16(out[2:4], uint16(totalLen))
	copy(out[4:20], p.Authenticator[:])
	copy(out[20:], attrsBody)

	// If a Message-Authenticator attribute is present, recompute it now
	// that the whole packet is laid out (with the MA value already at zero
	// because we just wrote attrsBody verbatim — we required the caller
	// to place it as 16 zero bytes).
	if maOffset, ok := findMessageAuthenticatorOffset(p.Attributes, 20); ok {
		// Validate the placeholder is the right length.
		if p.Attributes[maAttrIndex(p.Attributes)].Value == nil ||
			len(p.Attributes[maAttrIndex(p.Attributes)].Value) != MessageAuthenticatorLength {
			return nil, ErrInvalidMessageAuthLength
		}
		// Zero the MA value bytes inside `out` to be safe (in case caller
		// pre-filled them).
		for i := 0; i < MessageAuthenticatorLength; i++ {
			out[maOffset+i] = 0
		}
		mac := ComputeMessageAuthenticator(out, p.Secret)
		copy(out[maOffset:maOffset+MessageAuthenticatorLength], mac[:])
		// Keep the in-memory attribute value in sync with what's on the wire.
		copy(p.Attributes[maAttrIndex(p.Attributes)].Value, mac[:])
	}

	return out, nil
}

// Decode parses raw bytes into a Packet, performing only structural
// validation (length, attribute framing). Cryptographic validation
// (Validate*Authenticator) is the caller's responsibility.
func Decode(data, secret []byte) (*Packet, error) {
	if len(data) < MinPacketLength {
		return nil, ErrPacketTooShort
	}
	if len(data) > MaxPacketLength {
		return nil, ErrPacketTooLong
	}
	declared := int(binary.BigEndian.Uint16(data[2:4]))
	if declared < MinPacketLength || declared > len(data) {
		return nil, ErrLengthMismatch
	}

	p := &Packet{
		Code:       Code(data[0]),
		Identifier: data[1],
		Secret:     secret,
	}
	copy(p.Authenticator[:], data[4:20])

	attrs, err := decodeAttributes(data[20:declared])
	if err != nil {
		return nil, err
	}
	p.Attributes = attrs
	return p, nil
}

// ValidateMessageAuthenticator verifies the HMAC-MD5 in the
// Message-Authenticator attribute (RFC 2869 §5.14) against the packet bytes.
// Returns true if present and valid; returns false if missing or invalid.
//
// The caller must supply the original wire bytes because the in-memory
// Packet has already had the MA value parsed.
func (p *Packet) ValidateMessageAuthenticator(wireBytes, secret []byte) bool {
	idx := maAttrIndex(p.Attributes)
	if idx < 0 {
		return false
	}
	if len(p.Attributes[idx].Value) != MessageAuthenticatorLength {
		return false
	}
	provided := append([]byte(nil), p.Attributes[idx].Value...)

	// Build a copy of wireBytes with the MA value zeroed.
	maOffset, ok := findMessageAuthenticatorOffset(p.Attributes, 20)
	if !ok {
		return false
	}
	scratch := append([]byte(nil), wireBytes...)
	if maOffset+MessageAuthenticatorLength > len(scratch) {
		return false
	}
	for i := 0; i < MessageAuthenticatorLength; i++ {
		scratch[maOffset+i] = 0
	}

	want := ComputeMessageAuthenticator(scratch, secret)
	return constantTimeEqual(provided, want[:])
}

// ValidateResponseAuthenticator verifies the Authenticator of a reply
// against its request authenticator and the shared secret.
func (p *Packet) ValidateResponseAuthenticator(req *Packet, secret []byte, wireBytes []byte) bool {
	if !p.Code.IsResponse() {
		return false
	}
	if len(wireBytes) < MinPacketLength {
		return false
	}
	declared := binary.BigEndian.Uint16(wireBytes[2:4])
	body := wireBytes[20:declared]
	want := ComputeResponseAuthenticator(p.Code, p.Identifier, declared, req.Authenticator, body, secret)
	return constantTimeEqual(p.Authenticator[:], want[:])
}

// helpers ---------------------------------------------------------------

func isZeroAuth(a [16]byte) bool {
	for _, b := range a {
		if b != 0 {
			return false
		}
	}
	return true
}

// constantTimeEqual returns true if a and b are byte-equal in constant time.
func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// maAttrIndex returns the index of the first Message-Authenticator
// attribute, or -1 if absent.
func maAttrIndex(list AttributeList) int {
	for i, a := range list {
		if a.Type == AttrMessageAuthenticator {
			return i
		}
	}
	return -1
}

// findMessageAuthenticatorOffset returns the byte offset of the
// Message-Authenticator value within the encoded packet (skipping the
// 2-byte TL prefix), starting from `attrSectionOffset` (typically 20).
func findMessageAuthenticatorOffset(list AttributeList, attrSectionOffset int) (int, bool) {
	off := attrSectionOffset
	for _, a := range list {
		if a.Type == AttrMessageAuthenticator {
			return off + 2, true
		}
		off += 2 + len(a.Value)
	}
	return 0, false
}
