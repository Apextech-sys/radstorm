// Package radius — attribute types, encoding, and AttributeList accessors.
//
// Purpose:
//
//	Defines the standard RADIUS attribute type IDs we emit/consume, the
//	Attribute and VendorAttribute types, and the AttributeList helper
//	with typed Get* accessors. Wire encoding/decoding of the attribute
//	section happens in packet.go via this file's helpers.
//
// Related files:
//   - pkg/radius/packet.go  (assembles AttributeList into wire bytes)
//   - pkg/radius/vendor.go  (Huawei VSA + generic vendor helpers)
//   - docs/PROTOCOL.md      (canonical list of attributes we emit)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: AttributeType constants and the Attribute / AttributeList
// public API are consumed by pkg/io, pkg/subscriber, pkg/server. Changes
// require coordinated updates.
package radius

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// AttributeType is the 1-byte standard attribute type identifier (RFC 2865 §5).
type AttributeType uint8

// Standard attribute types radstorm emits or parses. List per docs/PROTOCOL.md.
const (
	AttrUserName            AttributeType = 1
	AttrUserPassword        AttributeType = 2
	AttrCHAPPassword        AttributeType = 3
	AttrNASIPAddress        AttributeType = 4
	AttrNASPort             AttributeType = 5
	AttrServiceType         AttributeType = 6
	AttrFramedProtocol      AttributeType = 7
	AttrFramedIPAddress     AttributeType = 8
	AttrReplyMessage        AttributeType = 18
	AttrCalledStationID     AttributeType = 30
	AttrCallingStationID    AttributeType = 31
	AttrNASIdentifier       AttributeType = 32
	AttrAcctStatusType      AttributeType = 40
	AttrAcctSessionID       AttributeType = 44
	AttrAcctAuthentic       AttributeType = 45
	AttrAcctSessionTime     AttributeType = 46
	AttrCHAPChallenge       AttributeType = 60
	AttrNASPortType         AttributeType = 61
	AttrVendorSpecific      AttributeType = 26
	AttrNASPortID           AttributeType = 87
	AttrMessageAuthenticator AttributeType = 80
	AttrErrorCause          AttributeType = 101
)

// Common Service-Type values (RFC 2865 §5.6).
const (
	ServiceTypeLogin         uint32 = 1
	ServiceTypeFramed        uint32 = 2
	ServiceTypeCallbackLogin uint32 = 3
)

// Common Framed-Protocol values (RFC 2865 §5.7).
const (
	FramedProtocolPPP uint32 = 1
	FramedProtocolSLIP uint32 = 2
)

// Common NAS-Port-Type values (RFC 2865 §5.41).
const (
	NASPortTypeAsync    uint32 = 0
	NASPortTypeEthernet uint32 = 15
	NASPortTypeVirtual  uint32 = 5
)

// Acct-Status-Type values (RFC 2866 §5.1).
const (
	AcctStatusStart           uint32 = 1
	AcctStatusStop            uint32 = 2
	AcctStatusInterimUpdate   uint32 = 3
	AcctStatusAccountingOn    uint32 = 7
	AcctStatusAccountingOff   uint32 = 8
)

// AcctAuthentic values (RFC 2866 §5.6).
const (
	AcctAuthenticRADIUS uint32 = 1
	AcctAuthenticLocal  uint32 = 2
	AcctAuthenticRemote uint32 = 3
)

// Error-Cause values (RFC 5176 §3.5) used when NAKing.
const (
	ErrorCauseUnsupportedAttribute    uint32 = 401
	ErrorCauseMissingAttribute        uint32 = 402
	ErrorCauseNASIdentificationMismatch uint32 = 403
	ErrorCauseInvalidRequest          uint32 = 404
	ErrorCauseSessionContextNotFound  uint32 = 503
	ErrorCauseResourcesUnavailable    uint32 = 506
)

// Attribute is a single RADIUS attribute (Type-Length-Value triple on the wire).
// Value is the raw bytes (no TL prefix). For Vendor-Specific (type 26) the
// Value contains the vendor-id+VSA payload; use the vendor.go helpers to
// build/parse them rather than hand-crafting bytes.
type Attribute struct {
	Type  AttributeType
	Value []byte
}

// AttributeList is an ordered list of attributes preserving wire order.
type AttributeList []Attribute

// MaxAttributeValueLength is the maximum bytes an attribute Value can hold
// (RFC 2865 §5: attributes are 255 bytes max including the 2-byte TL header).
const MaxAttributeValueLength = 253

// Add appends an attribute. Returns an error if Value exceeds wire limits.
func (a *AttributeList) Add(typ AttributeType, value []byte) error {
	if len(value) > MaxAttributeValueLength {
		return fmt.Errorf("attribute %d value length %d exceeds %d", typ, len(value), MaxAttributeValueLength)
	}
	*a = append(*a, Attribute{Type: typ, Value: append([]byte(nil), value...)})
	return nil
}

// AddString is a convenience for string-valued attributes (User-Name, etc.).
func (a *AttributeList) AddString(typ AttributeType, value string) error {
	return a.Add(typ, []byte(value))
}

// AddUint32 adds a 4-byte big-endian integer attribute (Service-Type, etc.).
func (a *AttributeList) AddUint32(typ AttributeType, value uint32) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], value)
	return a.Add(typ, b[:])
}

// AddIPv4 adds an IPv4-address attribute (NAS-IP-Address, Framed-IP-Address).
func (a *AttributeList) AddIPv4(typ AttributeType, ip net.IP) error {
	v4 := ip.To4()
	if v4 == nil {
		return fmt.Errorf("attribute %d: %v is not a valid IPv4 address", typ, ip)
	}
	return a.Add(typ, v4)
}

// Get returns the first attribute matching typ. Use for single-value attrs.
func (a AttributeList) Get(typ AttributeType) (Attribute, bool) {
	for _, attr := range a {
		if attr.Type == typ {
			return attr, true
		}
	}
	return Attribute{}, false
}

// GetAll returns every attribute matching typ in wire order. Use for attrs
// that legitimately repeat (e.g. Reply-Message, multiple VSAs).
func (a AttributeList) GetAll(typ AttributeType) []Attribute {
	var out []Attribute
	for _, attr := range a {
		if attr.Type == typ {
			out = append(out, attr)
		}
	}
	return out
}

// GetString returns the value of the first matching attribute as a string.
// Returns "" if not present.
func (a AttributeList) GetString(typ AttributeType) string {
	if attr, ok := a.Get(typ); ok {
		return string(attr.Value)
	}
	return ""
}

// GetUint32 returns the first matching attribute as a 4-byte big-endian uint32.
// Returns (0, false) if not present or wrong length.
func (a AttributeList) GetUint32(typ AttributeType) (uint32, bool) {
	attr, ok := a.Get(typ)
	if !ok || len(attr.Value) != 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(attr.Value), true
}

// GetIPv4 returns the first matching attribute as an IPv4 net.IP.
func (a AttributeList) GetIPv4(typ AttributeType) (net.IP, bool) {
	attr, ok := a.Get(typ)
	if !ok || len(attr.Value) != 4 {
		return nil, false
	}
	return net.IPv4(attr.Value[0], attr.Value[1], attr.Value[2], attr.Value[3]).To4(), true
}

// Remove deletes every attribute of the given type, returning the number removed.
func (a *AttributeList) Remove(typ AttributeType) int {
	out := (*a)[:0]
	removed := 0
	for _, attr := range *a {
		if attr.Type == typ {
			removed++
			continue
		}
		out = append(out, attr)
	}
	*a = out
	return removed
}

// encodeAttributes serializes the attribute list to wire format.
// Each attribute is [Type(1)][Length(1)][Value(0..253)]; total length must fit
// in the packet length cap (RFC 2865: Length ≤ 4096).
func encodeAttributes(list AttributeList) ([]byte, error) {
	out := make([]byte, 0, 64)
	for i, attr := range list {
		if len(attr.Value) > MaxAttributeValueLength {
			return nil, fmt.Errorf("attribute %d (type %d) value length %d exceeds %d",
				i, attr.Type, len(attr.Value), MaxAttributeValueLength)
		}
		out = append(out, byte(attr.Type), byte(2+len(attr.Value)))
		out = append(out, attr.Value...)
	}
	return out, nil
}

// decodeAttributes parses the attribute section of a packet body.
// Returns ErrTruncatedAttribute if a TL header overruns the input.
func decodeAttributes(data []byte) (AttributeList, error) {
	var list AttributeList
	for len(data) > 0 {
		if len(data) < 2 {
			return nil, ErrTruncatedAttribute
		}
		typ := AttributeType(data[0])
		length := int(data[1])
		if length < 2 || length > len(data) {
			return nil, ErrTruncatedAttribute
		}
		value := append([]byte(nil), data[2:length]...)
		list = append(list, Attribute{Type: typ, Value: value})
		data = data[length:]
	}
	return list, nil
}

// ErrTruncatedAttribute means an attribute claimed more bytes than remained.
var ErrTruncatedAttribute = errors.New("radius: truncated attribute in packet body")
