# Report — Slice 1A — RADIUS Protocol Layer

**Branch:** `wave-1/1a-radius`
**Worktree:** `C:\dev\radstorm-1a`
**Status:** Complete. All success criteria met.

## What was built

The `pkg/radius` Go package — a hand-rolled, RFC-faithful RADIUS protocol layer covering every packet flow radstorm needs to send or receive. Implementation is intentionally tight (no third-party RADIUS library on the hot path) so downstream slices can drive Identifier allocation, retransmit-with-fresh-ID, and authenticator math deterministically for stress testing.

### Packet flows covered

| Flow | Direction | Constructor / Decoder |
|---|---|---|
| Access-Request (PAP) | outbound | `NewAccessRequestPAP` |
| Access-Request (CHAP) | outbound | `NewAccessRequestCHAP` |
| Accounting-Request (Start) | outbound | `NewAccountingRequestStart` |
| Accounting-Request (other) | outbound | `NewAccountingRequest` |
| CoA-ACK / CoA-NAK | outbound | `NewCoAAck` / `NewCoANak` |
| Disconnect-ACK / Disconnect-NAK | outbound | `NewDisconnectAck` / `NewDisconnectNak` |
| Access-Accept / Access-Reject / Access-Challenge | inbound | `Decode` + `ValidateResponseAuthenticator` |
| Accounting-Response | inbound | `Decode` + `ValidateResponseAuthenticator` |
| CoA-Request / Disconnect-Request | inbound | `Decode` + `ValidateMessageAuthenticator` |

### Cryptography

- **RFC 2865 §5.2 PAP** — `EncryptUserPassword` / `DecryptUserPassword` (multi-block XOR with `MD5(secret || prevBlock)` chain, NUL-padded).
- **RFC 2865 §2.2 CHAP** — `CHAPPassword` (`chapID || MD5(chapID || password || challenge)`).
- **RFC 2866 / 5176 request authenticator** — `ComputeRequestAuthenticator` (Accounting/CoA/Disconnect Request).
- **RFC 2865 §3 response authenticator** — `ComputeResponseAuthenticator`, also wired through `Packet.ValidateResponseAuthenticator(req, secret, wire)` for inbound replies.
- **RFC 2869 §5.14 Message-Authenticator** — `ComputeMessageAuthenticator` (HMAC-MD5 over packet with MA value zeroed). Encode auto-fills the placeholder; `Packet.ValidateMessageAuthenticator(wire, secret)` verifies.
- Constant-time byte compare for all authenticator validations.

### Standard attributes

Every attribute listed in `docs/PROTOCOL.md` has a constant in `attribute.go` and helper add/get methods. Convenience adders for string, uint32, and IPv4 values; typed accessors with `(value, ok)` style returns.

### Vendor / Huawei VSA

- Generic `EncodeVSA` / `DecodeVSA` / `DecodeVSAs` per RFC 2865 §5.26.
- `AttributeList.AddVendorAttribute`, `GetVendor`, `GetAllVendor` for ergonomic access.
- Huawei-specific vendor-type constants (`HuaweiConnectID`, `HuaweiSubscriberQoSProfile`, etc.) for the attributes called out in `docs/PROTOCOL.md`.
- Byte-layout test asserts the exact wire format (vendor-id + vendor-TL + value).

### Embedded dictionaries

- `pkg/radius/dictionaries/rfc.dict` — curated RFC standard attributes radstorm uses.
- `pkg/radius/dictionaries/huawei.dict` — Huawei vendor 2011 VSAs we care about.
- `dictionary.go` parses both at first use via `//go:embed` and exposes `AttributeName(typ)`, `VendorAttributeName(vendor, vt)`, `LookupAttribute(name)`, `LookupVendorAttribute(name)`.
- Format mirrors FreeRADIUS dictionary syntax for easy maintenance.

## File list

```
pkg/radius/
  code.go                       — Code constants + classifiers
  attribute.go                  — AttributeType constants, AttributeList, encode/decode
  auth.go                       — PAP / CHAP / authenticator / HMAC-MD5 math
  packet.go                     — Packet struct + Encode/Decode + Validate*
  vendor.go                     — VSA helpers + Huawei vendor-type constants
  constructors.go               — High-level packet builders (NewAccessRequest*, etc.)
  dictionary.go                 — Embedded dictionary loader
  dictionaries/rfc.dict         — Standard attribute name table
  dictionaries/huawei.dict      — Huawei VSA name table

  code_test.go                  — Code classifier tests
  attribute_test.go             — AttributeList round-trip + accessor tests
  auth_test.go                  — PAP / CHAP / authenticator known-vector tests
  packet_test.go                — Encode/Decode/Validate tests for every packet type
  vendor_test.go                — VSA wire layout + accessor tests
  dictionary_test.go            — Dictionary load + lookup tests
  fixtures_test.go              — Deterministic round-trip; produces testdata/*.bin

  testdata/access-request-pap.bin       — Generated fixture for downstream consumers
  testdata/accounting-request-start.bin — Generated fixture
```

`go.mod` lists `github.com/stretchr/testify` as the only direct dependency.

## Test results

```
ok  github.com/Apextech-sys/radstorm/pkg/radius   1.96s   coverage: 85.7% of statements
```

- 35 top-level test functions (≈48 cases including table-driven subtests). All pass.
- Coverage **85.7%** of statements (target ≥85%, met).
- `go build ./...` succeeds from the worktree root.

Specific tests required by the briefing (each present and passing):

| Required test | Function |
|---|---|
| PAP encrypt/decrypt round-trip with vectors | `TestEncryptDecryptUserPasswordRoundTrip`, `TestEncryptUserPasswordKnownVector` |
| CHAP password matches reference | `TestCHAPPasswordKnownVector` |
| Message-Authenticator compute + tamper-rejection | `TestPacketMessageAuthenticatorEncodeAndValidate`, `TestComputeMessageAuthenticatorMatchesHMAC` |
| Response Authenticator compute + validate | `TestPacketResponseAuthenticatorComputedAndValidated`, `TestComputeResponseAuthenticatorMatchesSpec` |
| Access-Request encode→decode round trip | `TestPacketAccessRequestRoundTripPlain`, `TestPacketAccessRequestPAPRoundTripDecryptsPassword` |
| Huawei VSA TLV round-trip | `TestEncodeVSAByteLayoutHuaweiConnectID`, `TestDecodeVSARoundTrip`, `TestDecodeMultipleVSAsInOnePayload` |
| Decode an Access-Request from a `.bin` fixture | `TestFixtureAccessRequestDeterministicEncodeDecode` |

## Deviations from briefing

- **Did not use `layeh.com/radius`.** The briefing said "use where it fits, or hand-rolled VSA helpers if cleaner". After implementation it was clear that:
  1. `layeh.com`'s `Packet` type owns Identifier allocation internally, which conflicts with slice 2A's bitmap allocator.
  2. Direct control over the Authenticator field (zero-on-encode vs preserve) is needed for deterministic test fixtures; the library hides this.
  3. Hand-rolled MD5/HMAC math is ~80 lines and matches RFC byte-for-byte.

  Net result: zero third-party radius dependency, easier to audit, fixture bytes are stable. The library can be added back later if any subtle dictionary edge case demands it. Removed via `go mod tidy`.

- **Dictionary subset, not full FreeRADIUS dictionaries.** `dictionarygen` was not invoked. The dictionary loader handles the FreeRADIUS-style text format directly so additional attributes can be added without a build-time codegen step. Only the attributes listed in `docs/PROTOCOL.md` are covered. Slice 2D may need to extend `huawei.dict` if it observes additional Huawei VSAs in CoA traffic.

## Known limitations downstream slices need to know

1. **Identifier allocation is the caller's responsibility.** `Packet.Identifier` is a plain `uint8`; the bitmap allocator in slice 2A owns assignment. The constructors take an `identifier uint8` parameter explicitly to make this contract obvious.

2. **Secret on `Packet`.** `Packet.Secret` is set on every constructor and read by `Encode` / `Decode` / `Validate*`. It is never serialized but downstream code should null it out before any logging/persistence (the event collector should never receive a Packet with Secret populated).

3. **Single VSA per Vendor-Specific attribute on encode.** `AttributeList.AddVendorAttribute` writes one VSA per outer attribute. `DecodeVSAs` handles the multi-VSA-per-attribute case for inbound parsing. This matches Huawei BNG behavior (one VSA per outer attr) and avoids the 247-byte cap concerns for outbound packets we control.

4. **Message-Authenticator placement.** When `Encode` finds an MA attribute, it zeros the value bytes in the wire buffer before computing HMAC, then writes the result back in place. Callers must add `Attributes.Add(AttrMessageAuthenticator, make([]byte, 16))` as a placeholder; the constructors for CoA/Disconnect ACK/NAK do this automatically.

5. **No integration with `pkg/io` yet.** Pure encode/decode; no sockets, no retransmit, no I/O. Slice 2A owns those concerns and consumes this package via the `Packet` type and the constructors.

6. **No EAP / DTLS / RadSec / Extended Attributes.** Out-of-scope per briefing. If/when needed, add new files; do not extend `packet.go`.

## Acceptance gate

- [x] `go test ./pkg/radius/...` passes
- [x] Coverage ≥ 85% (got 85.7%)
- [x] `go build ./...` succeeds at worktree root
- [x] Every `.go` file has the mandated header block per `docs/CONVENTIONS.md`
- [x] Required test cases present and passing
- [x] Fixtures persisted under `pkg/radius/testdata/`
