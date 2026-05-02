// Package config — tests for the custom validators.
//
// Purpose:
//
//	Direct unit tests for hostport and file validators, independent of
//	TOML decoding, so their behaviour is pinned regardless of how
//	they're surfaced through Validate.
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

// We exercise the validators by validating tiny throwaway structs.

type hpHolder struct {
	Addr string `validate:"required,hostport"`
}

func TestHostport_Valid(t *testing.T) {
	v, err := getValidator()
	require.NoError(t, err)

	cases := []string{
		"127.0.0.1:1812",
		"localhost:8080",
		"example.com:65535",
		"0.0.0.0:1",
		"[::1]:1812",
	}
	for _, c := range cases {
		assert.NoError(t, v.Struct(hpHolder{Addr: c}), "expected %q valid", c)
	}
}

func TestHostport_Invalid(t *testing.T) {
	v, err := getValidator()
	require.NoError(t, err)

	cases := []string{
		"",
		"127.0.0.1",
		":1812",
		"127.0.0.1:0",
		"127.0.0.1:99999",
		"127.0.0.1:abc",
		"   :1812",
	}
	for _, c := range cases {
		assert.Error(t, v.Struct(hpHolder{Addr: c}), "expected %q invalid", c)
	}
}

type fileHolder struct {
	Path string `validate:"required,file"`
}

func TestFile_Valid(t *testing.T) {
	v, err := getValidator()
	require.NoError(t, err)

	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	require.NoError(t, os.WriteFile(p, []byte("hi"), 0o644))

	assert.NoError(t, v.Struct(fileHolder{Path: p}))
}

func TestFile_Invalid(t *testing.T) {
	v, err := getValidator()
	require.NoError(t, err)

	dir := t.TempDir() // exists but is a directory, not a regular file

	cases := []string{
		"",
		filepath.Join(dir, "missing.txt"),
		dir,
	}
	for _, c := range cases {
		assert.Error(t, v.Struct(fileHolder{Path: c}), "expected %q invalid", c)
	}
}
