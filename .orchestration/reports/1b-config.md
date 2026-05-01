# Report 1B — Config package

**Status:** done
**Branch:** `wave-1/1b-config`
**Worktree:** `C:\dev\radstorm-1b`

## What was built

- `pkg/config` — TOML-backed configuration package, schema-frozen against
  `.orchestration/contracts/config-schema.md`.
- Public API exactly as specified in the briefing:
  - `Load(path string) (*Config, []Credential, error)`
  - `LoadConfigOnly(path string) (*Config, error)`
  - `ApplyDefaults(*Config)` (and `ApplyDefaultsWithMeta` for the
    metadata-aware path the loader actually uses)
  - `Validate(*Config) error`
  - `LoadCredentials(path string) ([]Credential, error)`
  - `Credential` struct
- Struct definitions are a field-for-field, tag-for-tag mirror of the
  contract (toml + validate tags).
- Defaults table from the contract is applied; the loader uses
  `toml.MetaData.IsDefined` to distinguish "user wrote 0" from "user
  omitted the field".
- Custom validators registered with `validator/v10`:
  - `hostport` — parses with `net.SplitHostPort`, requires non-empty
    host and port in 1..65535. IPv6 literals must be bracketed.
  - `file` — `os.Stat` + `IsRegular`.
- Credentials CSV loader:
  - Required columns: `username`, `password`. Anything else is optional.
  - Case-insensitive, whitespace-tolerant header.
  - Optional cells default to empty strings; `auth_method` and
    `sub_type` are normalised to lower case and validated against
    their enum.
  - Blank rows are skipped silently.
  - `Load(path)` checks that `len(creds) >= subscribers.count` and
    returns a clear error otherwise.
- Every Go file carries the file-header block per `docs/CONVENTIONS.md`.

## Files created

- `pkg/config/config.go`
- `pkg/config/load.go`
- `pkg/config/validate.go`
- `pkg/config/credentials.go`
- `pkg/config/load_test.go`
- `pkg/config/validate_test.go`
- `pkg/config/credentials_test.go`
- `pkg/config/testdata/example.toml`
- `pkg/config/testdata/minimal_defaults.toml`
- `pkg/config/testdata/missing_secret.toml`
- `pkg/config/testdata/bad_pap_pct.toml`
- `pkg/config/testdata/bad_scenario_type.toml`
- `pkg/config/testdata/bad_hostport.toml`
- `pkg/config/testdata/bad_credentials_path.toml`
- `pkg/config/testdata/bad_toml.toml`
- `pkg/config/testdata/credentials/small.csv`
- `pkg/config/testdata/credentials/minimal.csv`
- `pkg/config/testdata/credentials/missing_password_col.csv`
- `pkg/config/testdata/credentials/bad_auth_method.csv`
- `pkg/config/testdata/credentials/empty.csv`
- `pkg/config/testdata/credentials/1k.csv` (generated, 1000 rows)

## Files modified

- `go.mod`, `go.sum` — added `BurntSushi/toml`,
  `go-playground/validator/v10`, `stretchr/testify` and their transitive
  deps.

## Tests

```
go test ./pkg/config/... -v -cover
```

- 29 tests, all PASS.
- Coverage: **91.3 %** of statements (target ≥85 %).

`go build ./...` and `go vet ./pkg/config/...` both succeed (exit 0).

### Success criteria coverage

| Criterion (from briefing)                                                                 | Test                                       |
|-------------------------------------------------------------------------------------------|--------------------------------------------|
| Loading the docs example returns a fully-populated Config                                 | `TestLoad_Example_FullyPopulated`          |
| Loading a config with missing optional fields applies defaults                            | `TestLoadConfigOnly_AppliesDefaults`       |
| `target.shared_secret = ""` returns a validation error mentioning that field              | `TestLoad_MissingSharedSecret`             |
| `auth_method_pap_pct = 150` fails validation                                              | `TestLoad_BadPapPct`                       |
| 1000-row credentials CSV returns 1000 Credentials                                         | `TestLoadCredentials_BulkOneThousand`      |
| CSV missing required columns returns a clear error                                        | `TestLoadCredentials_MissingRequiredColumn`|
| CSV with optional columns absent returns Credentials with empty optional fields          | `TestLoadCredentials_OptionalColumnsAbsent`|

## Deviations from briefing

- Added `ApplyDefaultsWithMeta(*Config, *toml.MetaData)` alongside the
  briefed `ApplyDefaults(*Config)`. The metadata-aware variant is what
  the loader uses; the public `ApplyDefaults` is a thin wrapper kept
  for the test API the briefing specified. This was necessary so that
  a user-written `0` (e.g. `max_retries = 0`) is preserved — without
  metadata, "zero" and "absent" are indistinguishable.
- Exported `Validate` (not in the briefing's listed surface). It's
  needed by the future `validate-config` CLI subcommand and made tests
  cleaner. No downside; can be made internal later if a future review
  prefers.

## Follow-ups

- None blocking Wave 2.
- When the `validate-config` CLI subcommand lands in Wave 3, it can
  also run pre-flight checks (socket bind, target reachability) — the
  briefing explicitly excluded those from this slice.
- The `pkg/config` exports a stable surface; downstream slices (1D
  frontend types, 1F API skeleton) can begin generating their mirror
  TypeScript types from `contracts/config-schema.md` as planned.
