// Package radius — embedded dictionary loader for attribute name lookups.
//
// Purpose:
//
//	Loads the curated RFC + Huawei dictionaries embedded in the binary at
//	package init() time and exposes Name lookups used by logging and the
//	event collector. Dictionaries are simple text format mirroring
//	FreeRADIUS' dictionary syntax.
//
// Related files:
//   - pkg/radius/dictionaries/rfc.dict      (standard attribute names)
//   - pkg/radius/dictionaries/huawei.dict   (Huawei VSAs)
//   - pkg/radius/vendor.go                  (decodes VSAs whose names live here)
//
// Briefing: .orchestration/briefings/1a-radius-protocol.md
//
// Contract: AttributeName(typ) and VendorAttributeName(vendor, vt) are
// public lookup helpers. Returning "" for unknown is intentional — callers
// fall back to numeric IDs.
package radius

import (
	"bufio"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

//go:embed dictionaries/rfc.dict
var rfcDictBytes []byte

//go:embed dictionaries/huawei.dict
var huaweiDictBytes []byte

// AttributeMeta is a parsed dictionary entry.
type AttributeMeta struct {
	Name string
	Type AttributeType
	Kind string // "string" | "integer" | "ipaddr" | "octets" | "date"
}

// VendorAttributeMeta is a parsed VSA dictionary entry.
type VendorAttributeMeta struct {
	VendorID   uint32
	VendorName string
	VendorType uint8
	Name       string
	Kind       string
}

var (
	dictOnce sync.Once
	dictErr  error

	stdAttrByID   = map[AttributeType]AttributeMeta{}
	stdAttrByName = map[string]AttributeMeta{}

	vendorByID   = map[uint32]string{}
	vendorByName = map[string]uint32{}

	vsaByID   = map[uint64]VendorAttributeMeta{} // (vendor<<8) | vendorType
	vsaByName = map[string]VendorAttributeMeta{}
)

func loadDicts() {
	dictOnce.Do(func() {
		if err := parseStdDict(string(rfcDictBytes)); err != nil {
			dictErr = fmt.Errorf("parse rfc.dict: %w", err)
			return
		}
		if err := parseVendorDict(string(huaweiDictBytes)); err != nil {
			dictErr = fmt.Errorf("parse huawei.dict: %w", err)
		}
	})
}

func parseStdDict(content string) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "ATTRIBUTE" {
			continue
		}
		name := fields[1]
		idVal, err := strconv.ParseUint(fields[2], 10, 8)
		if err != nil {
			return fmt.Errorf("line %d: invalid id %q: %w", lineNo, fields[2], err)
		}
		meta := AttributeMeta{
			Name: name,
			Type: AttributeType(idVal),
			Kind: fields[3],
		}
		stdAttrByID[meta.Type] = meta
		stdAttrByName[name] = meta
	}
	return scanner.Err()
}

func parseVendorDict(content string) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "VENDOR":
			if len(fields) < 3 {
				return fmt.Errorf("line %d: VENDOR requires name and id", lineNo)
			}
			vid, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil {
				return fmt.Errorf("line %d: invalid vendor id %q: %w", lineNo, fields[2], err)
			}
			vendorByID[uint32(vid)] = fields[1]
			vendorByName[fields[1]] = uint32(vid)
		case "VENDOR-ATTRIBUTE":
			if len(fields) < 5 {
				return fmt.Errorf("line %d: VENDOR-ATTRIBUTE requires vendor, name, type, kind", lineNo)
			}
			vendorName := fields[1]
			vid, ok := vendorByName[vendorName]
			if !ok {
				return fmt.Errorf("line %d: unknown VENDOR %q", lineNo, vendorName)
			}
			attrName := fields[2]
			vt, err := strconv.ParseUint(fields[3], 10, 8)
			if err != nil {
				return fmt.Errorf("line %d: invalid vendor-type %q: %w", lineNo, fields[3], err)
			}
			meta := VendorAttributeMeta{
				VendorID:   vid,
				VendorName: vendorName,
				VendorType: uint8(vt),
				Name:       attrName,
				Kind:       fields[4],
			}
			vsaByID[(uint64(vid)<<8)|uint64(vt)] = meta
			vsaByName[attrName] = meta
		}
	}
	return scanner.Err()
}

// AttributeName returns the human-readable name for a standard attribute,
// or "" if the type is unknown.
func AttributeName(t AttributeType) string {
	loadDicts()
	if meta, ok := stdAttrByID[t]; ok {
		return meta.Name
	}
	return ""
}

// VendorName returns the registered vendor name (e.g. "Huawei") for the
// vendor-id, or "" if unknown.
func VendorName(id uint32) string {
	loadDicts()
	return vendorByID[id]
}

// VendorAttributeName returns the human-readable name for a VSA, or
// "" if the (vendor, vendorType) pair is unknown.
func VendorAttributeName(vendor uint32, vendorType uint8) string {
	loadDicts()
	if meta, ok := vsaByID[(uint64(vendor)<<8)|uint64(vendorType)]; ok {
		return meta.Name
	}
	return ""
}

// LookupAttribute returns the parsed metadata for a standard attribute name.
func LookupAttribute(name string) (AttributeMeta, bool) {
	loadDicts()
	m, ok := stdAttrByName[name]
	return m, ok
}

// LookupVendorAttribute returns the parsed metadata for a VSA by name.
func LookupVendorAttribute(name string) (VendorAttributeMeta, bool) {
	loadDicts()
	m, ok := vsaByName[name]
	return m, ok
}

// DictionaryLoadError returns any error encountered during init()-style
// parse of the embedded dictionaries. nil means dictionaries loaded clean.
func DictionaryLoadError() error {
	loadDicts()
	return dictErr
}
