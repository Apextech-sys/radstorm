# Briefing 1B — Config package

## Mission

Build the `pkg/config` package that loads, validates, and supplies `*Config` to the rest of the system. The schema is fully specified — your job is implementation discipline.

## Context — read these first

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/CONVENTIONS.md` — file-header convention MANDATORY
3. **`.orchestration/contracts/config-schema.md`** — the schema is FROZEN; your Go struct must match it exactly
4. `docs/CONFIG.md` — human-readable reference

## Working directory

- **Worktree:** `C:\dev\radstorm` on `main` (no parallel writers to `pkg/config/` in Wave 1)
- **Files you own:**
  - `pkg/config/config.go` — the typed structs from the contract
  - `pkg/config/load.go` — TOML loading, defaults application
  - `pkg/config/validate.go` — validation using `github.com/go-playground/validator/v10`
  - `pkg/config/credentials.go` — credentials CSV loader
  - `pkg/config/*_test.go` — unit tests
  - `pkg/config/testdata/*.toml` — example configs (good + bad)
  - `pkg/config/testdata/credentials/*.csv`

## Scope

**In scope:**
- Define structs verbatim per `.orchestration/contracts/config-schema.md`
- TOML loader using `github.com/BurntSushi/toml`
- Apply defaults table from the contract
- Validate with `validator/v10` using struct tags
- Custom validation for: `hostport` (host:port format), `file` (path exists)
- Credentials loader: CSV parser using `encoding/csv`, returns `[]Credential`. Handle quoting, missing optional columns.
- Function: `Load(path string) (*Config, []Credential, error)` — single entry point

**Out of scope:**
- Pre-flight checks like socket binding (that's a CLI command in Wave 3)
- Threshold evaluation (collector's job)

## Public API

```go
package config

func Load(path string) (*Config, []Credential, error)         // load and validate
func LoadConfigOnly(path string) (*Config, error)              // for validate-config command
func ApplyDefaults(*Config)                                    // separately exposed for tests

type Credential struct {
    Username   string
    Password   string
    AuthMethod string  // "" if not specified, else "pap" | "chap"
    SubType    string  // "" or "pppoe" | "mac"
    NASPortID  string
    MACAddress string
}
```

## Success criteria

- `go test ./pkg/config/...` passes with ≥85% coverage
- These tests exist and pass:
  - Loading the example from `docs/CONFIG.md` returns a fully-populated Config
  - Loading a config with missing optional fields applies defaults correctly
  - Loading a config with `target.shared_secret = ""` returns a validation error mentioning that field
  - Loading a config with invalid `auth_method_pap_pct = 150` fails validation
  - Loading a credentials CSV with 1000 rows returns 1000 Credentials
  - Loading a CSV missing required columns returns a clear error
  - Loading a CSV with optional columns missing returns Credentials with empty optional fields

## Test requirements

- Standard `testing` + `testify`
- Fixtures under `pkg/config/testdata/`

## File-header requirement

Every file gets the header per `docs/CONVENTIONS.md`.

## Dependencies you may add

- `github.com/BurntSushi/toml`
- `github.com/go-playground/validator/v10`
- `github.com/stretchr/testify` (already added by 1A; reuse)

## Reporting

Write `.orchestration/reports/1b-config.md` per the standard template.
