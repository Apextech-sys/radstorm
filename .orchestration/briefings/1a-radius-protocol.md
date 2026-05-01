# Briefing 1A — RADIUS protocol layer

## Mission

Build the `pkg/radius` Go package that owns RADIUS packet encoding/decoding for everything radstorm needs to send and receive. This is foundational — every subsequent slice depends on it.

## Context — read these first

1. `README.md`
2. `.orchestration/STATE.md` and `.orchestration/WAVES.md`
3. `docs/ARCHITECTURE.md` — note the `pkg/radius` boundary
4. `docs/PROTOCOL.md` — authoritative protocol reference
5. `docs/CONVENTIONS.md` — file-header convention is MANDATORY

## Working directory

- **Worktree:** `C:\dev\radstorm` directly on `main` for this slice (no parallel writers to `pkg/radius/` in Wave 1)
- **Files you own:**
  - `go.mod`, `go.sum` (initialize the module as `github.com/Apextech-sys/reflex-radstorm`)
  - `pkg/radius/*.go` — packet types, encode/decode, attribute helpers, secret/authenticator math
  - `pkg/radius/dictionaries/*.dict` — RFC standard + Huawei dictionary (vendor 2011)
  - `pkg/radius/dictionary.go` — embedded dictionary loader using `//go:embed`
  - `pkg/radius/*_test.go` — unit tests with vector fixtures
  - `pkg/radius/testdata/*.bin` — captured packet fixtures for round-trip tests

## Scope

**In scope:**
- Initialize Go module: `go mod init github.com/Apextech-sys/reflex-radstorm`
- Use Go 1.22+ features
- Add `layeh.com/radius` as a dependency (battle-tested RADIUS library)
- Compile in standard RFC dictionary + Huawei dictionary (vendor 2011) using `layeh.com/radius/dictionarygen` OR by writing minimal hand-rolled attribute helpers if the generator is too heavy. Prefer the generator.
- Provide encode/decode for these packet flows:
  - **Outbound:** Access-Request (PAP), Access-Request (CHAP), Accounting-Request (Acct-Status-Type=Start), CoA-ACK, CoA-NAK, Disconnect-ACK, Disconnect-NAK
  - **Inbound parsing:** Access-Accept, Access-Reject, Accounting-Response, CoA-Request, Disconnect-Request
- PAP encryption per RFC 2865 §5.2
- CHAP-Password computation per RFC 2865 §2.2
- Message-Authenticator (HMAC-MD5) compute & verify per RFC 2869
- Response Authenticator validation on every inbound reply
- Helpers for emitting the standard attribute set listed in `docs/PROTOCOL.md` (User-Name, NAS-IP-Address, NAS-Port, Service-Type, Framed-Protocol, Called-Station-Id, Calling-Station-Id, NAS-Identifier, NAS-Port-Type, NAS-Port-Id, Acct-Status-Type, Acct-Session-Id, Acct-Authentic, Framed-IP-Address)
- Huawei VSA encode/decode for the attributes listed in `docs/PROTOCOL.md` (at minimum decode for inbound CoA; encode optional for now)
- Public API surface should be small and obvious — see "Suggested API" below

**Out of scope:**
- Sockets / I/O (that's slice 2A)
- Subscriber state (that's slice 2B)
- EAP, DTLS, RadSec, Extended Attributes

## Suggested public API

```go
// Generic packet wrapper
type Packet struct {
    Code          Code
    Identifier    uint8
    Authenticator [16]byte
    Attributes    AttributeList
    Secret        []byte // shared secret, used for encode/decode but never serialized
}

func (p *Packet) Encode() ([]byte, error)
func Decode(data []byte, secret []byte) (*Packet, error)

// Constructors for what we send
func NewAccessRequestPAP(identifier uint8, secret []byte, username, password string, attrs ...Attribute) *Packet
func NewAccessRequestCHAP(identifier uint8, secret []byte, username, password string, attrs ...Attribute) *Packet
func NewAccountingRequestStart(identifier uint8, secret []byte, attrs ...Attribute) *Packet
func NewCoAAck(req *Packet, secret []byte) *Packet
func NewCoANak(req *Packet, secret []byte, errorCause uint32) *Packet
func NewDisconnectAck(req *Packet, secret []byte) *Packet
func NewDisconnectNak(req *Packet, secret []byte, errorCause uint32) *Packet

// Validation
func (p *Packet) ValidateMessageAuthenticator(secret []byte) bool
func (p *Packet) ValidateResponseAuthenticator(req *Packet, secret []byte) bool

// Attribute access
type AttributeList []Attribute
func (a AttributeList) Get(typ AttributeType) (Attribute, bool)
func (a AttributeList) GetVendor(vendor uint32, typ uint8) (VendorAttribute, bool)
func (a AttributeList) GetString(typ AttributeType) string
// etc.
```

## Success criteria

- `go build ./...` succeeds
- `go test ./pkg/radius/...` passes
- Unit test coverage ≥85% for `pkg/radius`
- These specific tests exist and pass:
  - PAP encryption: encrypt + decrypt round-trip with known vectors
  - CHAP password: computed value matches an external reference (compute one with `radclient`-style logic offline and embed the expected bytes)
  - Message-Authenticator: compute on a known packet, verify against a known result; tamper one byte, verify rejection
  - Response Authenticator: encode an Access-Accept reply with a known request, verify the bytes match what FreeRADIUS would emit
  - Round-trip: encode an Access-Request, decode it back, ensure all attributes survive
  - Huawei VSA: encode at least one TLV-style attribute (e.g. Huawei-Connect-Id), decode it, ensure round-trip
  - Decode an Access-Request from a captured `.bin` fixture (you generate the fixtures using `radclient` against the local Docker rig in slice 1E — for Wave 1 you can use any well-formed fixture, the more thorough validation comes in 4A)

## Test requirements

- Use `testing` + `github.com/stretchr/testify/assert` and `require`
- Place fixtures under `pkg/radius/testdata/`
- For Huawei VSA tests, document the byte layout you're testing against in a comment

## File-header requirement

EVERY `.go` file gets the header per `docs/CONVENTIONS.md`. No exceptions. This includes test files.

## Dependencies you may add

- `layeh.com/radius`
- `layeh.com/radius/rfc2865`, `layeh.com/radius/rfc2866`, etc. as needed
- `github.com/stretchr/testify`

Avoid adding anything else without justification.

## Reporting

Write `.orchestration/reports/1a-radius-protocol.md`:
- What was built (packets supported, attributes covered, dictionary status)
- File list
- `go test ./pkg/radius/...` output (counts, coverage)
- Any deviations from this brief
- Any known limitations downstream slices need to know about
