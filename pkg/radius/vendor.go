// Package radius — Vendor-Specific Attribute (VSA) encoding/decoding helpers.
//
// Purpose:
//
//	Wraps the RFC 2865 §5.26 Vendor-Specific (attribute 26) framing and
//	provides a typed lookup of named Huawei VSAs (vendor-id 2011) that
//	radstorm cares about. Pure encoding; no I/O.
//
// Related files:
//   - pkg/radius/attribute.go           (AttributeList that holds VSAs)
//   - pkg/radius/dictionary.go          (loads the human-readable name table)
//   - pkg/radius/dictionaries/huawei.dict
//   - docs/PROTOCOL.md                  (table of Huawei VSAs we care about)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: VendorAttribute, VendorHuawei constant, EncodeVSA / DecodeVSA
// signatures are public. Callers in pkg/server depend on these to read
// inbound CoA packets.
package radius

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// VendorHuawei is the IANA-assigned Private Enterprise Number for Huawei.
const VendorHuawei uint32 = 2011

// VendorAttribute is the decoded form of a single VSA carried inside a
// Vendor-Specific (type 26) attribute.
type VendorAttribute struct {
	VendorID   uint32
	VendorType uint8
	Value      []byte
}

// Common Huawei VSAs we expect to receive in CoA-Request packets, per
// docs/PROTOCOL.md. Values are the Huawei-specific Vendor-Type byte (the
// outer Type is always 26 = Vendor-Specific).
const (
	HuaweiInputPeakRate         uint8 = 1
	HuaweiInputAverageRate      uint8 = 2
	HuaweiInputBasicRate        uint8 = 3
	HuaweiOutputPeakRate        uint8 = 5
	HuaweiSubscriberQoSProfile  uint8 = 18
	HuaweiConnectID             uint8 = 20
	HuaweiAcctSessionID         uint8 = 26
	HuaweiServiceType           uint8 = 65
)

// ErrInvalidVSA means the VSA payload is malformed (length too short or
// internal Vendor-Length disagrees with the outer attribute length).
var ErrInvalidVSA = errors.New("radius: invalid VSA payload")

// EncodeVSA encodes a single VSA into a Vendor-Specific attribute Value
// suitable for AttributeList.Add(AttrVendorSpecific, ...).
//
// On-wire layout (RFC 2865 §5.26):
//
//	[VendorID(4 bytes, big-endian)]
//	[VendorType(1)]
//	[VendorLength(1, includes itself + VendorType + Value)]
//	[Value(...)]
//
// Maximum Value length is 247 bytes (255 - 4 vendor-id - 2 vendor TL - 2
// outer TL = 247).
func EncodeVSA(vendorID uint32, vendorType uint8, value []byte) ([]byte, error) {
	const maxVSAValue = 247
	if len(value) > maxVSAValue {
		return nil, fmt.Errorf("radius: VSA value length %d exceeds %d", len(value), maxVSAValue)
	}
	out := make([]byte, 4+2+len(value))
	binary.BigEndian.PutUint32(out[0:4], vendorID)
	out[4] = vendorType
	out[5] = byte(2 + len(value))
	copy(out[6:], value)
	return out, nil
}

// DecodeVSA parses a single VSA from the Value field of a Vendor-Specific
// (type 26) attribute. Returns ErrInvalidVSA if malformed.
//
// NOTE: Some vendors pack multiple VSAs into one Vendor-Specific attribute.
// For radstorm's needs (Huawei in particular emits one VSA per outer
// attribute) this single-VSA decode is sufficient. Use DecodeVSAs for the
// general case.
func DecodeVSA(value []byte) (VendorAttribute, error) {
	if len(value) < 6 {
		return VendorAttribute{}, ErrInvalidVSA
	}
	vendorID := binary.BigEndian.Uint32(value[0:4])
	vendorType := value[4]
	vendorLength := int(value[5])
	if vendorLength < 2 || 4+vendorLength > len(value) {
		return VendorAttribute{}, ErrInvalidVSA
	}
	return VendorAttribute{
		VendorID:   vendorID,
		VendorType: vendorType,
		Value:      append([]byte(nil), value[6:4+vendorLength]...),
	}, nil
}

// DecodeVSAs parses one or more VSAs from a Vendor-Specific attribute
// Value field. Returns the list in wire order.
func DecodeVSAs(value []byte) ([]VendorAttribute, error) {
	if len(value) < 4 {
		return nil, ErrInvalidVSA
	}
	vendorID := binary.BigEndian.Uint32(value[0:4])
	rest := value[4:]
	var out []VendorAttribute
	for len(rest) > 0 {
		if len(rest) < 2 {
			return nil, ErrInvalidVSA
		}
		vt := rest[0]
		vl := int(rest[1])
		if vl < 2 || vl > len(rest) {
			return nil, ErrInvalidVSA
		}
		out = append(out, VendorAttribute{
			VendorID:   vendorID,
			VendorType: vt,
			Value:      append([]byte(nil), rest[2:vl]...),
		})
		rest = rest[vl:]
	}
	return out, nil
}

// AddVendorAttribute is a convenience that EncodeVSA's then Adds a VSA
// to the list as a Vendor-Specific (type 26) attribute.
func (a *AttributeList) AddVendorAttribute(vendorID uint32, vendorType uint8, value []byte) error {
	encoded, err := EncodeVSA(vendorID, vendorType, value)
	if err != nil {
		return err
	}
	return a.Add(AttrVendorSpecific, encoded)
}

// GetVendor returns the first Vendor-Specific attribute matching
// vendorID + vendorType. Returns (zero, false) if absent.
func (a AttributeList) GetVendor(vendorID uint32, vendorType uint8) (VendorAttribute, bool) {
	for _, attr := range a {
		if attr.Type != AttrVendorSpecific {
			continue
		}
		vsas, err := DecodeVSAs(attr.Value)
		if err != nil {
			continue
		}
		for _, v := range vsas {
			if v.VendorID == vendorID && v.VendorType == vendorType {
				return v, true
			}
		}
	}
	return VendorAttribute{}, false
}

// GetAllVendor returns every VSA matching vendorID (any vendor-type).
func (a AttributeList) GetAllVendor(vendorID uint32) []VendorAttribute {
	var out []VendorAttribute
	for _, attr := range a {
		if attr.Type != AttrVendorSpecific {
			continue
		}
		vsas, err := DecodeVSAs(attr.Value)
		if err != nil {
			continue
		}
		for _, v := range vsas {
			if v.VendorID == vendorID {
				out = append(out, v)
			}
		}
	}
	return out
}
