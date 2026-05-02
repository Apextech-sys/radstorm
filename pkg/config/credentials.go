// Package config — CSV-backed credentials loader.
//
// Purpose:
//   Parses the credentials file referenced by Subscribers.CredentialsFile.
//   Required header columns are username and password; auth_method,
//   sub_type, nas_port_id and mac_address are optional and default to
//   empty strings when their column is absent or the cell is blank.
//
// Related files:
//   - pkg/config/load.go (calls LoadCredentials from the public Load entry point)
//   - .orchestration/contracts/config-schema.md (Credentials file format section)
//
// Briefing: .orchestration/briefings/1b-config.md
//
// Contract:
//   Public type Credential and function LoadCredentials. The Credential
//   struct shape is consumed by pkg/scenario when building the subscriber
//   pool — adding fields here without touching that contract is fine,
//   but renames must coordinate.
package config

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Credential is one row from the credentials CSV. Optional columns are
// returned as empty strings when absent in the source file.
type Credential struct {
	Username   string
	Password   string
	AuthMethod string
	SubType    string
	NASPortID  string
	MACAddress string
}

// canonical column names. Keep the lookup case-insensitive but tolerate
// surrounding whitespace in the header row.
const (
	colUsername   = "username"
	colPassword   = "password"
	colAuthMethod = "auth_method"
	colSubType    = "sub_type"
	colNASPortID  = "nas_port_id"
	colMACAddress = "mac_address"
)

// LoadCredentials reads the CSV at path and returns one Credential per
// non-empty data row. The file MUST have a header row containing at least
// the username and password columns.
func LoadCredentials(path string) ([]Credential, error) {
	if path == "" {
		return nil, errors.New("credentials path is empty")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening credentials file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate optional trailing columns
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("credentials file %q is empty", path)
		}
		return nil, fmt.Errorf("reading header of %q: %w", path, err)
	}

	idx := indexHeader(header)

	if _, ok := idx[colUsername]; !ok {
		return nil, fmt.Errorf("credentials file %q missing required column %q", path, colUsername)
	}
	if _, ok := idx[colPassword]; !ok {
		return nil, fmt.Errorf("credentials file %q missing required column %q", path, colPassword)
	}

	var creds []Credential
	rowNum := 1 // header was line 1
	for {
		record, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading row %d of %q: %w", rowNum+1, path, err)
		}
		rowNum++

		// Skip blank rows entirely (all empty fields).
		if isBlankRecord(record) {
			continue
		}

		c := Credential{
			Username:   field(record, idx, colUsername),
			Password:   field(record, idx, colPassword),
			AuthMethod: strings.ToLower(field(record, idx, colAuthMethod)),
			SubType:    strings.ToLower(field(record, idx, colSubType)),
			NASPortID:  field(record, idx, colNASPortID),
			MACAddress: field(record, idx, colMACAddress),
		}
		if c.Username == "" {
			return nil, fmt.Errorf("row %d of %q: empty username", rowNum, path)
		}
		if c.Password == "" {
			return nil, fmt.Errorf("row %d of %q: empty password", rowNum, path)
		}
		if c.AuthMethod != "" && c.AuthMethod != "pap" && c.AuthMethod != "chap" {
			return nil, fmt.Errorf("row %d of %q: invalid auth_method %q (want pap|chap)", rowNum, path, c.AuthMethod)
		}
		if c.SubType != "" && c.SubType != "pppoe" && c.SubType != "mac" {
			return nil, fmt.Errorf("row %d of %q: invalid sub_type %q (want pppoe|mac)", rowNum, path, c.SubType)
		}
		creds = append(creds, c)
	}

	return creds, nil
}

func indexHeader(header []string) map[string]int {
	out := make(map[string]int, len(header))
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		if key == "" {
			continue
		}
		// First occurrence wins; later duplicates are ignored.
		if _, exists := out[key]; !exists {
			out[key] = i
		}
	}
	return out
}

func field(record []string, idx map[string]int, name string) string {
	i, ok := idx[name]
	if !ok || i >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[i])
}

func isBlankRecord(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
