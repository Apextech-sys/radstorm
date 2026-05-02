// Package server — focused unit tests for the small pure helpers in
// handler.go (VSA tag keying, value rendering, standard-attribute tag
// extraction). These exercise edge cases that the end-to-end UDP tests
// in listener_test.go don't easily reach.
//
// Related files:
//   - pkg/server/handler.go (the helpers under test)
//
// Briefing: .orchestration/briefings/2c-server-listener.md
//
// Contract: tests; no public surface.
package server

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

func TestVsaTagKeyForEveryHuaweiName(t *testing.T) {
	cases := []struct {
		vt   uint8
		want string
	}{
		{radius.HuaweiInputPeakRate, "huawei_input_peak_rate"},
		{radius.HuaweiInputAverageRate, "huawei_input_average_rate"},
		{radius.HuaweiInputBasicRate, "huawei_input_basic_rate"},
		{radius.HuaweiOutputPeakRate, "huawei_output_peak_rate"},
		{radius.HuaweiSubscriberQoSProfile, "huawei_subscriber_qos_profile"},
		{radius.HuaweiConnectID, "huawei_connect_id"},
		{radius.HuaweiAcctSessionID, "huawei_acct_session_id"},
		{radius.HuaweiServiceType, "huawei_service_type"},
	}
	for _, tc := range cases {
		got := vsaTagKey(radius.VendorAttribute{
			VendorID:   radius.VendorHuawei,
			VendorType: tc.vt,
		})
		assert.Equal(t, tc.want, got, "vendor type %d", tc.vt)
	}
}

func TestVsaTagKeyForUnknownVendorOrType(t *testing.T) {
	// Unknown vendor entirely — falls back to vsa_<vendor>_<type>.
	got := vsaTagKey(radius.VendorAttribute{VendorID: 9999, VendorType: 7})
	assert.Equal(t, "vsa_9999_7", got)

	// Known Huawei vendor, unknown sub-type — also falls back.
	got = vsaTagKey(radius.VendorAttribute{VendorID: radius.VendorHuawei, VendorType: 254})
	assert.Equal(t, "vsa_2011_254", got)
}

func TestVsaTagValueDecimalFor4Bytes(t *testing.T) {
	v := vsaTagValue(radius.VendorAttribute{Value: []byte{0x00, 0x00, 0x00, 0x10}})
	assert.Equal(t, "16", v)

	v = vsaTagValue(radius.VendorAttribute{Value: []byte{0xff, 0xff, 0xff, 0xff}})
	assert.Equal(t, "4294967295", v)
}

func TestVsaTagValuePrintableASCIIRendersAsString(t *testing.T) {
	v := vsaTagValue(radius.VendorAttribute{Value: []byte("session-12345")})
	assert.Equal(t, "session-12345", v)
}

func TestVsaTagValueBinaryRendersAsHex(t *testing.T) {
	// 5 bytes (not a uint32-shaped value) and not all printable.
	v := vsaTagValue(radius.VendorAttribute{Value: []byte{0xde, 0xad, 0xbe, 0xef, 0x01}})
	assert.Equal(t, "0xdeadbeef01", v)
}

func TestVsaTagValueEmptyRendersAsEmptyHex(t *testing.T) {
	v := vsaTagValue(radius.VendorAttribute{Value: []byte{}})
	assert.Equal(t, "0x", v)
}

func TestIsPrintableASCII(t *testing.T) {
	assert.True(t, isPrintableASCII([]byte("hello world")))
	assert.True(t, isPrintableASCII([]byte("session/123-abc.xyz")))
	assert.False(t, isPrintableASCII([]byte{}), "empty must be false (no point rendering nothing as a string)")
	assert.False(t, isPrintableASCII([]byte{0x00}))
	assert.False(t, isPrintableASCII([]byte{0x1f}))
	assert.False(t, isPrintableASCII([]byte{0x7f}))
	assert.False(t, isPrintableASCII([]byte{0xff}))
	assert.False(t, isPrintableASCII([]byte("ok\n")))
}

func TestStandardTagsExtractsEveryKnownAttribute(t *testing.T) {
	pkt := &radius.Packet{Code: radius.CodeCoARequest}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "alice"))
	require.NoError(t, pkt.Attributes.AddString(radius.AttrAcctSessionID, "session-789"))
	require.NoError(t, pkt.Attributes.AddString(radius.AttrCallingStationID, "AA:BB:CC:DD:EE:FF"))
	require.NoError(t, pkt.Attributes.AddIPv4(radius.AttrFramedIPAddress, net.ParseIP("10.0.0.42")))
	require.NoError(t, pkt.Attributes.AddIPv4(radius.AttrNASIPAddress, net.ParseIP("192.168.1.1")))
	require.NoError(t, pkt.Attributes.AddString(radius.AttrNASIdentifier, "bng-01"))

	tags := standardTags(pkt)
	assert.Equal(t, "alice", tags["user_name"])
	assert.Equal(t, "session-789", tags["acct_session_id"])
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", tags["calling_station_id"])
	assert.Equal(t, "10.0.0.42", tags["framed_ip"])
	assert.Equal(t, "192.168.1.1", tags["nas_ip"])
	assert.Equal(t, "bng-01", tags["nas_identifier"])
}

func TestStandardTagsOmitsAbsentAttributes(t *testing.T) {
	pkt := &radius.Packet{Code: radius.CodeCoARequest}
	require.NoError(t, pkt.Attributes.AddString(radius.AttrUserName, "alice"))

	tags := standardTags(pkt)
	assert.Equal(t, "alice", tags["user_name"])
	_, hasNasIP := tags["nas_ip"]
	assert.False(t, hasNasIP)
	_, hasFramedIP := tags["framed_ip"]
	assert.False(t, hasFramedIP)
}

func TestCopyTagsIsADeepEnoughCopyForOurUse(t *testing.T) {
	in := map[string]string{"a": "1", "b": "2"}
	out := copyTags(in)
	out["a"] = "MODIFIED"
	assert.Equal(t, "1", in["a"], "input must not be mutated when caller mutates the copy")
	assert.Equal(t, "MODIFIED", out["a"])
	assert.Equal(t, "2", out["b"])
}

func TestVsaTagsAggregatesMultipleVendorAttributes(t *testing.T) {
	pkt := &radius.Packet{Code: radius.CodeCoARequest}

	// Two distinct VSAs: Huawei rate + Huawei session ID.
	rate, err := radius.EncodeVSA(radius.VendorHuawei, radius.HuaweiInputAverageRate, []byte{0x00, 0x00, 0x10, 0x00})
	require.NoError(t, err)
	sess, err := radius.EncodeVSA(radius.VendorHuawei, radius.HuaweiAcctSessionID, []byte("hwsess-1"))
	require.NoError(t, err)
	require.NoError(t, pkt.Attributes.Add(radius.AttrVendorSpecific, rate))
	require.NoError(t, pkt.Attributes.Add(radius.AttrVendorSpecific, sess))

	tags, vsaErr := vsaTags(pkt)
	require.NoError(t, vsaErr)
	assert.Equal(t, "4096", tags["huawei_input_average_rate"])
	assert.Equal(t, "hwsess-1", tags["huawei_acct_session_id"])
}

func TestVsaTagsSurfacesUnknownVendorAsFallbackKey(t *testing.T) {
	pkt := &radius.Packet{Code: radius.CodeCoARequest}
	custom, err := radius.EncodeVSA(54321, 7, []byte{0xab})
	require.NoError(t, err)
	require.NoError(t, pkt.Attributes.Add(radius.AttrVendorSpecific, custom))

	tags, vsaErr := vsaTags(pkt)
	require.NoError(t, vsaErr)
	assert.Equal(t, "0xab", tags["vsa_54321_7"])
}
