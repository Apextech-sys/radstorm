// Package config — tests for the credentials CSV loader.
//
// Purpose:
//
//	Pin LoadCredentials behaviour: required vs. optional columns, blank
//	row tolerance, validation of enum-style optional cells, and a 1k-row
//	bulk-load smoke test.
//
// Briefing: .orchestration/briefings/1b-config.md
//
// Contract: internal — test code only.
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCredentials_FullColumns(t *testing.T) {
	creds, err := LoadCredentials("testdata/credentials/small.csv")
	require.NoError(t, err)
	require.Len(t, creds, 4)

	assert.Equal(t, "sub00000001", creds[0].Username)
	assert.Equal(t, "pw00000001", creds[0].Password)
	assert.Equal(t, "pap", creds[0].AuthMethod)
	assert.Equal(t, "pppoe", creds[0].SubType)
	assert.Equal(t, "1/1/1.100", creds[0].NASPortID)
	assert.Equal(t, "aa:bb:cc:00:00:01", creds[0].MACAddress)

	assert.Equal(t, "chap", creds[1].AuthMethod)

	// Row 3: nas_port_id blank, mac present.
	assert.Equal(t, "", creds[2].NASPortID)
	assert.Equal(t, "aa:bb:cc:00:00:03", creds[2].MACAddress)

	// Row 4: all optional cells blank.
	assert.Equal(t, "sub00000004", creds[3].Username)
	assert.Equal(t, "pw00000004", creds[3].Password)
	assert.Equal(t, "", creds[3].AuthMethod)
	assert.Equal(t, "", creds[3].SubType)
	assert.Equal(t, "", creds[3].NASPortID)
	assert.Equal(t, "", creds[3].MACAddress)
}

func TestLoadCredentials_OptionalColumnsAbsent(t *testing.T) {
	creds, err := LoadCredentials("testdata/credentials/minimal.csv")
	require.NoError(t, err)
	require.Len(t, creds, 3)

	for _, c := range creds {
		assert.NotEmpty(t, c.Username)
		assert.NotEmpty(t, c.Password)
		assert.Empty(t, c.AuthMethod)
		assert.Empty(t, c.SubType)
		assert.Empty(t, c.NASPortID)
		assert.Empty(t, c.MACAddress)
	}
}

func TestLoadCredentials_MissingRequiredColumn(t *testing.T) {
	_, err := LoadCredentials("testdata/credentials/missing_password_col.csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password")
}

func TestLoadCredentials_BadAuthMethod(t *testing.T) {
	_, err := LoadCredentials("testdata/credentials/bad_auth_method.csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_method")
}

func TestLoadCredentials_EmptyFile(t *testing.T) {
	_, err := LoadCredentials("testdata/credentials/empty.csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadCredentials_EmptyPath(t *testing.T) {
	_, err := LoadCredentials("")
	require.Error(t, err)
}

func TestLoadCredentials_NonexistentFile(t *testing.T) {
	_, err := LoadCredentials("testdata/credentials/does-not-exist.csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening credentials file")
}

func TestLoadCredentials_BulkOneThousand(t *testing.T) {
	creds, err := LoadCredentials("testdata/credentials/1k.csv")
	require.NoError(t, err)
	require.Len(t, creds, 1000)
	assert.Equal(t, "sub00000001", creds[0].Username)
	assert.Equal(t, "sub00001000", creds[999].Username)
}

func TestLoadCredentials_BlankRowsSkipped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "blanks.csv")
	body := "username,password\n" +
		"alice,a1\n" +
		"\n" +
		" , \n" +
		"bob,b2\n"
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	creds, err := LoadCredentials(p)
	require.NoError(t, err)
	require.Len(t, creds, 2)
	assert.Equal(t, "alice", creds[0].Username)
	assert.Equal(t, "bob", creds[1].Username)
}

func TestLoadCredentials_EmptyUsernameRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.csv")
	body := "username,password\n,nopw\n"
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	_, err := LoadCredentials(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty username")
}

func TestLoadCredentials_BadSubType(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.csv")
	body := "username,password,sub_type\nalice,pw,frame-relay\n"
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

	_, err := LoadCredentials(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sub_type")
}

func TestLoad_InsufficientCredentials(t *testing.T) {
	// example.toml has subscribers.count = 3 and uses minimal.csv with 3 rows
	// — Load should succeed there. To exercise the insufficient-rows branch,
	// build a tiny scratch config inline that points at a 3-row CSV but asks
	// for 1000 subscribers.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "scratch.toml")
	body := `
[target]
auth_address  = "127.0.0.1:1812"
acct_address  = "127.0.0.1:1813"
shared_secret = "s"

[coa_listener]
bind_address  = "0.0.0.0:3799"
shared_secret = "s"

[subscribers]
count            = 1000
credentials_file = "` + filepath.ToSlash(absPath(t, "testdata/credentials/minimal.csv")) + `"

[source]
ips = ["127.0.0.1"]
port_range = [10000, 60000]

[nas]
ip_address = "10.0.0.1"
identifier = "n"

[scenario]
type = "uniform"

[scenario.uniform]
duration_sec = 1.0
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o644))

	_, _, err := Load(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "need at least 1000")
}

func absPath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	require.NoError(t, err)
	return abs
}
