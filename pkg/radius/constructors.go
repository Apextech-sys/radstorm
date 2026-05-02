// Package radius — high-level constructors for the packets radstorm sends.
//
// Purpose:
//
//	Provides ergonomic builders for the specific packet flows in the
//	radstorm test (Access-Request PAP/CHAP, Accounting-Request Start,
//	CoA/Disconnect ACK and NAK). Each constructor returns a *Packet
//	ready for Encode().
//
// Related files:
//   - pkg/radius/packet.go        (Packet + Encode/Decode)
//   - pkg/radius/auth.go          (PAP/CHAP/authenticator math)
//   - pkg/radius/attribute.go     (attribute list + adders)
//   - pkg/subscriber              (consumer of these constructors)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: Constructor signatures are part of the public API and are
// called from pkg/subscriber and pkg/server. Adding new constructors is
// fine; changing existing signatures is a breaking change.
package radius

import (
	"crypto/rand"
	"fmt"
)

// NewAccessRequestPAP builds an Access-Request with a User-Password
// (PAP) attribute encrypted under the supplied secret. The Authenticator
// is generated cryptographically random; password is encrypted under it
// per RFC 2865 §5.2.
//
// Caller may pass extra attributes (NAS-IP-Address, Calling-Station-Id,
// etc.) which are appended after User-Name but before User-Password.
// Adding User-Password or CHAP-Password via extras is rejected — use the
// dedicated PAP/CHAP constructors.
func NewAccessRequestPAP(identifier uint8, secret []byte, username, password string, extras ...Attribute) (*Packet, error) {
	auth, err := NewRequestAuthenticator()
	if err != nil {
		return nil, err
	}

	p := &Packet{
		Code:          CodeAccessRequest,
		Identifier:    identifier,
		Authenticator: auth,
		Secret:        secret,
	}

	if err := p.Attributes.AddString(AttrUserName, username); err != nil {
		return nil, fmt.Errorf("add User-Name: %w", err)
	}
	for _, a := range extras {
		if a.Type == AttrUserPassword || a.Type == AttrCHAPPassword || a.Type == AttrCHAPChallenge {
			return nil, fmt.Errorf("radius: refusing to add credential attribute %d via extras", a.Type)
		}
		if err := p.Attributes.Add(a.Type, a.Value); err != nil {
			return nil, fmt.Errorf("add extra attribute %d: %w", a.Type, err)
		}
	}

	cipher, err := EncryptUserPassword([]byte(password), secret, auth)
	if err != nil {
		return nil, fmt.Errorf("encrypt User-Password: %w", err)
	}
	if err := p.Attributes.Add(AttrUserPassword, cipher); err != nil {
		return nil, fmt.Errorf("add User-Password: %w", err)
	}
	return p, nil
}

// NewAccessRequestCHAP builds an Access-Request using CHAP-Password. A
// fresh CHAP-Identifier and 16-byte CHAP-Challenge are generated; the
// CHAP-Challenge is included as attribute 60 so the server can verify
// independently of the Request Authenticator.
func NewAccessRequestCHAP(identifier uint8, secret []byte, username, password string, extras ...Attribute) (*Packet, error) {
	auth, err := NewRequestAuthenticator()
	if err != nil {
		return nil, err
	}

	var chapIDBytes [1]byte
	if _, err := rand.Read(chapIDBytes[:]); err != nil {
		return nil, fmt.Errorf("read CHAP-Identifier: %w", err)
	}
	chapID := chapIDBytes[0]

	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("read CHAP-Challenge: %w", err)
	}

	p := &Packet{
		Code:          CodeAccessRequest,
		Identifier:    identifier,
		Authenticator: auth,
		Secret:        secret,
	}

	if err := p.Attributes.AddString(AttrUserName, username); err != nil {
		return nil, fmt.Errorf("add User-Name: %w", err)
	}
	for _, a := range extras {
		if a.Type == AttrUserPassword || a.Type == AttrCHAPPassword || a.Type == AttrCHAPChallenge {
			return nil, fmt.Errorf("radius: refusing to add credential attribute %d via extras", a.Type)
		}
		if err := p.Attributes.Add(a.Type, a.Value); err != nil {
			return nil, fmt.Errorf("add extra attribute %d: %w", a.Type, err)
		}
	}

	chapPass := CHAPPassword(chapID, []byte(password), challenge)
	if err := p.Attributes.Add(AttrCHAPPassword, chapPass); err != nil {
		return nil, fmt.Errorf("add CHAP-Password: %w", err)
	}
	if err := p.Attributes.Add(AttrCHAPChallenge, challenge); err != nil {
		return nil, fmt.Errorf("add CHAP-Challenge: %w", err)
	}
	return p, nil
}

// NewAccountingRequestStart builds an Accounting-Request with
// Acct-Status-Type=Start. The Authenticator is computed by Encode per
// RFC 2866 §3 (MD5 over Code|Identifier|Length|0…0|Attributes|Secret).
//
// The caller must supply User-Name, NAS identification, Acct-Session-Id,
// etc. via extras — this constructor only wires Acct-Status-Type and the
// authenticator math.
func NewAccountingRequestStart(identifier uint8, secret []byte, extras ...Attribute) (*Packet, error) {
	p := &Packet{
		Code:       CodeAccountingRequest,
		Identifier: identifier,
		Secret:     secret,
	}
	if err := p.Attributes.AddUint32(AttrAcctStatusType, AcctStatusStart); err != nil {
		return nil, err
	}
	for _, a := range extras {
		if err := p.Attributes.Add(a.Type, a.Value); err != nil {
			return nil, fmt.Errorf("add extra attribute %d: %w", a.Type, err)
		}
	}
	return p, nil
}

// NewAccountingRequest is the generic form for non-Start accounting
// records (Stop, Interim-Update, Accounting-On/Off). statusType is one of
// the AcctStatus* constants.
func NewAccountingRequest(identifier uint8, secret []byte, statusType uint32, extras ...Attribute) (*Packet, error) {
	p := &Packet{
		Code:       CodeAccountingRequest,
		Identifier: identifier,
		Secret:     secret,
	}
	if err := p.Attributes.AddUint32(AttrAcctStatusType, statusType); err != nil {
		return nil, err
	}
	for _, a := range extras {
		if err := p.Attributes.Add(a.Type, a.Value); err != nil {
			return nil, fmt.Errorf("add extra attribute %d: %w", a.Type, err)
		}
	}
	return p, nil
}

// NewCoAAck builds a CoA-ACK reply for the given request. Includes a
// zeroed Message-Authenticator placeholder; Encode fills it in.
func NewCoAAck(req *Packet, secret []byte, extras ...Attribute) (*Packet, error) {
	return newDynAuthAck(CodeCoAACK, req, secret, extras...)
}

// NewCoANak builds a CoA-NAK reply with the given Error-Cause.
func NewCoANak(req *Packet, secret []byte, errorCause uint32, extras ...Attribute) (*Packet, error) {
	return newDynAuthNak(CodeCoANAK, req, secret, errorCause, extras...)
}

// NewDisconnectAck builds a Disconnect-ACK reply.
func NewDisconnectAck(req *Packet, secret []byte, extras ...Attribute) (*Packet, error) {
	return newDynAuthAck(CodeDisconnectACK, req, secret, extras...)
}

// NewDisconnectNak builds a Disconnect-NAK reply with the given Error-Cause.
func NewDisconnectNak(req *Packet, secret []byte, errorCause uint32, extras ...Attribute) (*Packet, error) {
	return newDynAuthNak(CodeDisconnectNAK, req, secret, errorCause, extras...)
}

// newDynAuthAck is the shared body for CoA-ACK / Disconnect-ACK.
func newDynAuthAck(code Code, req *Packet, secret []byte, extras ...Attribute) (*Packet, error) {
	if req == nil {
		return nil, fmt.Errorf("radius: nil request packet for %s", code)
	}
	p := &Packet{
		Code:                 code,
		Identifier:           req.Identifier,
		RequestAuthenticator: req.Authenticator,
		Secret:               secret,
	}
	for _, a := range extras {
		if err := p.Attributes.Add(a.Type, a.Value); err != nil {
			return nil, fmt.Errorf("add extra attribute %d: %w", a.Type, err)
		}
	}
	if err := p.Attributes.Add(AttrMessageAuthenticator, make([]byte, MessageAuthenticatorLength)); err != nil {
		return nil, err
	}
	return p, nil
}

// newDynAuthNak is the shared body for CoA-NAK / Disconnect-NAK.
func newDynAuthNak(code Code, req *Packet, secret []byte, errorCause uint32, extras ...Attribute) (*Packet, error) {
	if req == nil {
		return nil, fmt.Errorf("radius: nil request packet for %s", code)
	}
	p := &Packet{
		Code:                 code,
		Identifier:           req.Identifier,
		RequestAuthenticator: req.Authenticator,
		Secret:               secret,
	}
	if err := p.Attributes.AddUint32(AttrErrorCause, errorCause); err != nil {
		return nil, err
	}
	for _, a := range extras {
		if err := p.Attributes.Add(a.Type, a.Value); err != nil {
			return nil, fmt.Errorf("add extra attribute %d: %w", a.Type, err)
		}
	}
	if err := p.Attributes.Add(AttrMessageAuthenticator, make([]byte, MessageAuthenticatorLength)); err != nil {
		return nil, err
	}
	return p, nil
}
