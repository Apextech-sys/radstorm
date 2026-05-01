// Package radius — tests for embedded dictionary loader.
//
// Purpose:
//
//	Confirms the embedded RFC + Huawei dictionaries parse cleanly at
//	package init() time and that AttributeName / LookupAttribute return
//	expected entries.
//
// Related files:
//   - pkg/radius/dictionary.go
//   - pkg/radius/dictionaries/rfc.dict
//   - pkg/radius/dictionaries/huawei.dict
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: tests; no public surface.
package radius

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDictionaryLoadsCleanly(t *testing.T) {
	require.NoError(t, DictionaryLoadError())
}

func TestStandardAttributeNameLookups(t *testing.T) {
	assert.Equal(t, "User-Name", AttributeName(AttrUserName))
	assert.Equal(t, "User-Password", AttributeName(AttrUserPassword))
	assert.Equal(t, "NAS-IP-Address", AttributeName(AttrNASIPAddress))
	assert.Equal(t, "Message-Authenticator", AttributeName(AttrMessageAuthenticator))
	assert.Equal(t, "Acct-Status-Type", AttributeName(AttrAcctStatusType))
	assert.Equal(t, "Error-Cause", AttributeName(AttrErrorCause))
	assert.Equal(t, "", AttributeName(AttributeType(254)), "unknown attribute returns empty")
}

func TestLookupAttributeByName(t *testing.T) {
	meta, ok := LookupAttribute("User-Name")
	require.True(t, ok)
	assert.Equal(t, AttrUserName, meta.Type)
	assert.Equal(t, "string", meta.Kind)

	meta, ok = LookupAttribute("NAS-Port")
	require.True(t, ok)
	assert.Equal(t, AttrNASPort, meta.Type)
	assert.Equal(t, "integer", meta.Kind)

	_, ok = LookupAttribute("Bogus-Attr")
	assert.False(t, ok)
}

func TestLookupVendorAttributeByName(t *testing.T) {
	meta, ok := LookupVendorAttribute("Huawei-Connect-Id")
	require.True(t, ok)
	assert.Equal(t, VendorHuawei, meta.VendorID)
	assert.Equal(t, HuaweiConnectID, meta.VendorType)
	assert.Equal(t, "integer", meta.Kind)
}
