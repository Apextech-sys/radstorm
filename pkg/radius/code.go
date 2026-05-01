// Package radius — RADIUS protocol packet encoding/decoding for radstorm.
//
// Purpose:
//
//	Defines the RADIUS packet Code byte values per RFC 2865/2866/5176 used
//	by radstorm. This file is the canonical enumeration of codes the rest
//	of the package switches against.
//
// Related files:
//   - pkg/radius/packet.go  (uses Code in the Packet struct)
//   - docs/PROTOCOL.md      (table of supported codes)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: Code constants are part of the public API. Renaming or
// renumbering is a breaking change for every consumer in pkg/io,
// pkg/subscriber, pkg/server.
package radius

import "fmt"

// Code is the first byte of a RADIUS packet identifying packet type.
type Code uint8

// RADIUS packet codes used by radstorm. See RFC 2865/2866/5176.
const (
	CodeAccessRequest      Code = 1
	CodeAccessAccept       Code = 2
	CodeAccessReject       Code = 3
	CodeAccountingRequest  Code = 4
	CodeAccountingResponse Code = 5
	CodeAccessChallenge    Code = 11
	CodeDisconnectRequest  Code = 40
	CodeDisconnectACK      Code = 41
	CodeDisconnectNAK      Code = 42
	CodeCoARequest         Code = 43
	CodeCoAACK             Code = 44
	CodeCoANAK             Code = 45
)

// String returns the canonical name of the code, useful for logging.
func (c Code) String() string {
	switch c {
	case CodeAccessRequest:
		return "Access-Request"
	case CodeAccessAccept:
		return "Access-Accept"
	case CodeAccessReject:
		return "Access-Reject"
	case CodeAccountingRequest:
		return "Accounting-Request"
	case CodeAccountingResponse:
		return "Accounting-Response"
	case CodeAccessChallenge:
		return "Access-Challenge"
	case CodeDisconnectRequest:
		return "Disconnect-Request"
	case CodeDisconnectACK:
		return "Disconnect-ACK"
	case CodeDisconnectNAK:
		return "Disconnect-NAK"
	case CodeCoARequest:
		return "CoA-Request"
	case CodeCoAACK:
		return "CoA-ACK"
	case CodeCoANAK:
		return "CoA-NAK"
	default:
		return fmt.Sprintf("Unknown(%d)", uint8(c))
	}
}

// IsRequest reports whether the code is a request that carries a
// random Request Authenticator (Access-Request) or a computed one
// (Accounting/CoA/Disconnect Request) per RFC 2865/2866/5176.
func (c Code) IsRequest() bool {
	switch c {
	case CodeAccessRequest, CodeAccountingRequest, CodeCoARequest, CodeDisconnectRequest:
		return true
	default:
		return false
	}
}

// IsResponse reports whether the code is a reply whose authenticator is
// MD5(Code|Identifier|Length|RequestAuthenticator|Attributes|Secret).
func (c Code) IsResponse() bool {
	switch c {
	case CodeAccessAccept, CodeAccessReject, CodeAccessChallenge,
		CodeAccountingResponse,
		CodeCoAACK, CodeCoANAK,
		CodeDisconnectACK, CodeDisconnectNAK:
		return true
	default:
		return false
	}
}
