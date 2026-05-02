# ADR 0002 — Hand-rolled RADIUS protocol layer instead of layeh.com/radius

<!-- Purpose: Documents why pkg/radius is a bespoke implementation rather than wrapping the layeh.com/radius library, and the risk trade-offs accepted. -->

**Status:** Accepted

**Date:** 2026-05-02

---

## Context

The Wave 1A sub-agent was briefed to implement the RADIUS protocol layer (`pkg/radius`) covering RFC 2865/2866/5176 packet construction, authenticator math, and Huawei VSA handling. The briefing noted that `layeh.com/radius` could be used "where it fits."

Two fundamental constraints emerged during implementation:

1. **Identifier allocation ownership**: radstorm's I/O layer (`pkg/io`) owns a per-`(srcIP, srcPort, dstIP, dstPort)` 256-slot bitmap allocator for the 8-bit RADIUS Identifier. The `layeh.com/radius` `Packet` type manages its own Identifier field and exposes it for read but not for external bitmap-driven allocation. Wiring an external allocator around the library would require post-construction mutation, defeating the type safety the library provides.

2. **Deterministic test fixtures**: The authenticator math (Request Authenticator, Response Authenticator, Message-Authenticator HMAC-MD5) must produce stable, byte-for-byte reproducible outputs for test vector validation. The `layeh.com` library computes authenticators internally during encode, making it hard to assert intermediate values against RFC §3 test vectors.

---

## Decision

`pkg/radius` is hand-rolled: ~800 lines covering packet encode/decode, PAP/CHAP authentication, RFC 2865/2866/5176 authenticator computation, Message-Authenticator HMAC-MD5, and VSA helpers (including Huawei vendor 2011).

`layeh.com/radius` is not a dependency. `go.mod` has no RADIUS library.

Key design choices in the implementation:

- **Packet constructors take an explicit `identifier uint8` parameter.** The caller (the bitmap allocator in `pkg/io`) is responsible for allocation. Constructors do not generate random IDs.
- **`Encode` fills the Message-Authenticator in-place.** Callers add a 16-byte zero placeholder attribute; `Encode` zeros it, computes HMAC-MD5 over the full wire buffer, and writes the result back. This matches RFC 2869 §5.14 exactly.
- **Authenticator validation is constant-time.** `hmac.Equal` is used for all MAC comparisons.
- **Embedded dictionaries** (`pkg/radius/dictionaries/rfc.dict`, `huawei.dict`) use FreeRADIUS text format for easy extension without a code-generation step.

---

## Consequences

**Positive:**

- Full control over Identifier allocation: the bitmap allocator and the packet layer are cleanly decoupled. The allocator allocates an ID; the subscriber FSM passes it to a constructor; the packet carries it.
- Test fixtures are stable byte sequences that can be committed and validated against RFC examples.
- Zero third-party RADIUS dependency: the security-sensitive MD5/HMAC math is auditable in-repo without chasing a library.
- The implementation covers exactly the packet types radstorm needs. No unused code.

**Negative / trade-offs:**

- **Maintenance burden**: RFC-compliant RADIUS is well-understood but subtle (multi-block PAP encryption, exact authenticator field zeroing order). Any bugs in this layer are our bugs, not the library's. The 85.7% statement coverage on known-vector tests mitigates the most dangerous failure modes, but edge cases in the long tail of RFC attribute combinations are not covered.
- **Dictionary completeness**: The hand-curated dictionary covers only the attributes in `docs/PROTOCOL.md`. If a RADIUS server returns attributes not in our dictionary, they decode as opaque byte slices and are logged without a human-readable name. The FreeRADIUS dictionary is ~15,000 lines; ours is ~200.
- **No EAP, no DTLS, no RadSec**: These were explicitly out of scope. If any of these are needed, the package will need significant extension. The layeh library would have given them for free.

---

## Risk acceptance

The hand-rolled implementation is correct for the attributes and packet types that radstorm actually uses. The risk of subtle bugs in untested attribute combinations is accepted because:

- radstorm is a test harness, not a production NAS. An attribute decode error produces a logged warning; it does not compromise real subscriber sessions.
- The E2E test suite validates correctness against a real FreeRADIUS server. If the packet layer had a fundamental error, the 100% pass rate on the 100-subscriber smoke run would not be achievable.
- The implementation can be augmented with `layeh.com/radius` as a fallback for dictionary lookups if the need arises, without changing the core packet types.

---

## Alternatives considered

### `layeh.com/radius` as the primary library

Rejected (for the core hot path). The library's Identifier ownership model conflicts with the bitmap allocator design. Using it would require either forking the library to expose the allocator hook, or using its Identifier field and maintaining a separate shadow tracking structure — both worse than a clean hand-rolled implementation.

### `layeh.com/radius` for dictionary only

Possible future addition. The library's dictionary parser could replace our hand-coded `rfc.dict` / `huawei.dict` format without touching the packet layer. This was deferred because the current dictionary subset is small enough to maintain by hand and adding a dependency for dictionary lookup alone did not clear the cost/benefit bar.
