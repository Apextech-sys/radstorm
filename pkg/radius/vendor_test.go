// Package radius — tests for Vendor-Specific (VSA) encoding/decoding.
//
// Purpose:
//
//	Verifies the byte-for-byte VSA wire layout against an explicit
//	expected layout (documented inline) and round-trips Huawei VSAs
//	through the AttributeList helpers. Also exercises GetVendor /
//	GetAllVendor accessors.
//
// Related files:
//   - pkg/radius/vendor.go
//   - pkg/radius/dictionary.go
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeVSAByteLayoutHuaweiConnectID(t *testing.T) {
	// Huawei-Connect-Id is vendor-type 20 (0x14), kind=integer (4 bytes).
	// Expected wire layout for the inner VSA payload:
	//
	//   [0..3]  vendor-id    = 2011  → 0x00 0x00 0x07 0xDB
	//   [4]     vendor-type  = 20    → 0x14
	//   [5]     vendor-len   = 6     → 0x06   (covers vt + vl + 4-byte value)
	//   [6..9]  value        = 12345 → 0x00 0x00 0x30 0x39
	//
	// Total inner length = 10 bytes. The outer Vendor-Specific (type 26)
	// TL header (2 bytes) is added when this becomes an Attribute.
	value := make([]byte, 4)
	binary.BigEndian.PutUint32(value, 12345)

	encoded, err := EncodeVSA(VendorHuawei, HuaweiConnectID, value)
	require.NoError(t, err)

	want := []byte{
		0x00, 0x00, 0x07, 0xdb, // vendor-id 2011
		0x14,                   // vendor-type 20
		0x06,                   // vendor-length 6
		0x00, 0x00, 0x30, 0x39, // value 12345
	}
	assert.Equal(t, want, encoded)
}

func TestEncodeVSAOversizeRejected(t *testing.T) {
	// 248-byte value would push the VSA over the 247-byte cap.
	tooBig := make([]byte, 248)
	_, err := EncodeVSA(VendorHuawei, HuaweiConnectID, tooBig)
	assert.Error(t, err)
}

func TestDecodeVSARoundTrip(t *testing.T) {
	value := []byte{1, 2, 3, 4, 5}
	encoded, err := EncodeVSA(VendorHuawei, HuaweiSubscriberQoSProfile, value)
	require.NoError(t, err)

	decoded, err := DecodeVSA(encoded)
	require.NoError(t, err)
	assert.Equal(t, VendorHuawei, decoded.VendorID)
	assert.Equal(t, HuaweiSubscriberQoSProfile, decoded.VendorType)
	assert.Equal(t, value, decoded.Value)
}

func TestDecodeVSAMalformed(t *testing.T) {
	t.Run("too short", func(t *testing.T) {
		_, err := DecodeVSA([]byte{0, 0, 0, 1, 2}) // missing length byte
		assert.ErrorIs(t, err, ErrInvalidVSA)
	})
	t.Run("vendor-length too small", func(t *testing.T) {
		// vendor-id 2011, vendor-type 1, length 1 (illegal, must be ≥2).
		bad := []byte{0x00, 0x00, 0x07, 0xdb, 0x01, 0x01}
		_, err := DecodeVSA(bad)
		assert.ErrorIs(t, err, ErrInvalidVSA)
	})
	t.Run("vendor-length overruns", func(t *testing.T) {
		bad := []byte{0x00, 0x00, 0x07, 0xdb, 0x01, 0x10, 0xaa, 0xbb}
		_, err := DecodeVSA(bad)
		assert.ErrorIs(t, err, ErrInvalidVSA)
	})
}

func TestDecodeMultipleVSAsInOnePayload(t *testing.T) {
	// Build a payload with vendor-id=2011 and two TLVs: type=20 value=42, type=65 value=7.
	val1 := make([]byte, 4)
	binary.BigEndian.PutUint32(val1, 42)
	val2 := make([]byte, 4)
	binary.BigEndian.PutUint32(val2, 7)

	payload := []byte{0x00, 0x00, 0x07, 0xdb} // vendor-id
	payload = append(payload, byte(HuaweiConnectID), 6)
	payload = append(payload, val1...)
	payload = append(payload, byte(HuaweiServiceType), 6)
	payload = append(payload, val2...)

	got, err := DecodeVSAs(payload)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, HuaweiConnectID, got[0].VendorType)
	assert.Equal(t, val1, got[0].Value)
	assert.Equal(t, HuaweiServiceType, got[1].VendorType)
	assert.Equal(t, val2, got[1].Value)
}

func TestAttributeListVendorAccessors(t *testing.T) {
	var list AttributeList
	val := make([]byte, 4)
	binary.BigEndian.PutUint32(val, 999)
	require.NoError(t, list.AddVendorAttribute(VendorHuawei, HuaweiConnectID, val))

	v2 := make([]byte, 4)
	binary.BigEndian.PutUint32(v2, 1)
	require.NoError(t, list.AddVendorAttribute(VendorHuawei, HuaweiInputPeakRate, v2))

	got, ok := list.GetVendor(VendorHuawei, HuaweiConnectID)
	require.True(t, ok)
	assert.Equal(t, val, got.Value)

	_, ok = list.GetVendor(VendorHuawei, 99)
	assert.False(t, ok, "absent VSA should report missing")

	all := list.GetAllVendor(VendorHuawei)
	assert.Len(t, all, 2)
}

func TestVendorAttributeNameLookup(t *testing.T) {
	require.NoError(t, DictionaryLoadError())
	assert.Equal(t, "Huawei-Connect-Id", VendorAttributeName(VendorHuawei, HuaweiConnectID))
	assert.Equal(t, "Huawei-Subscriber-QoS-Profile", VendorAttributeName(VendorHuawei, HuaweiSubscriberQoSProfile))
	assert.Equal(t, "Huawei", VendorName(VendorHuawei))
	assert.Equal(t, "", VendorAttributeName(VendorHuawei, 254), "unknown VSA returns empty string")
	assert.Equal(t, "", VendorName(9999), "unknown vendor returns empty string")
}
