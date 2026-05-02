// Package config — validation orchestration and custom validators.
//
// Purpose:
//
//	Wraps go-playground/validator/v10 with two custom validators required
//	by the config schema: `hostport` (host:port string with non-empty host
//	and port in 1..65535) and `file` (path exists and is a regular file).
//
// Related files:
//   - pkg/config/config.go (struct tags reference these validators)
//   - pkg/config/load.go (calls Validate after applying defaults)
//   - .orchestration/contracts/config-schema.md
//
// Briefing: .orchestration/briefings/1b-config.md
//
// Contract: Internal — Validate is the only exported function and is meant
// to be called via Load/LoadConfigOnly, but it is exported so tests and
// the validate-config CLI subcommand can drive it directly.
package config

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

var (
	validateOnce sync.Once
	validateInst *validator.Validate
	validateErr  error
)

// Validate runs the playground validator over cfg using the registered
// custom validators (`hostport`, `file`) plus the standard library.
func Validate(cfg *Config) error {
	v, err := getValidator()
	if err != nil {
		return err
	}
	return v.Struct(cfg)
}

func getValidator() (*validator.Validate, error) {
	validateOnce.Do(func() {
		v := validator.New(validator.WithRequiredStructEnabled())
		if err := v.RegisterValidation("hostport", validateHostport); err != nil {
			validateErr = err
			return
		}
		if err := v.RegisterValidation("file", validateFileExists); err != nil {
			validateErr = err
			return
		}
		validateInst = v
	})
	return validateInst, validateErr
}

// validateHostport accepts strings of the form "host:port" where host is
// non-empty and port parses to an integer in 1..65535. IPv6 literals must
// be bracketed: "[::1]:1812".
func validateHostport(fl validator.FieldLevel) bool {
	s := fl.Field().String()
	if s == "" {
		return false
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return false
	}
	if strings.TrimSpace(host) == "" {
		return false
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return p >= 1 && p <= 65535
}

// validateFileExists succeeds when the field is a path to an existing
// regular file. Empty strings fail (combine with `required` if needed).
func validateFileExists(fl validator.FieldLevel) bool {
	path := fl.Field().String()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
