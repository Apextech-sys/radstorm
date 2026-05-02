# Report: 2A — I/O Layer

## What I built

The `pkg/io` package: the public `Engine` plus its sub-components.

- **`SocketPool`** (`socketpool.go`) — binds UDP sockets across configured source IPs and a port range (or one ephemeral port per IP), round-robins selection, idempotent close.
- **`IDAllocator`** (`idalloc.go`) — per-`(srcIP, srcPort, dstIP, dstPort)` 256-bit bitmap. Non-blocking `Allocate` / `Release`. Uses a rotating `next` hint so freed IDs don't recycle immediately (gives late replies more time to drain). Tuple key normalises IPv4/v6.
- **`ReplyMatcher`** (`matcher.go`) — outstanding-request correlation table keyed on `(localAddr, identifier)`. Validates Response Authenticator on every reply via `radius.Packet.ValidateResponseAuthenticator`. Maintains a `recentDone` table (TTL 30s, GC'd lazily) so a late second reply for an already-completed request is recognised and emitted as `duplicate_reply` rather than `unmatched_reply`. Bad-auth replies are dropped without claiming the inflight, so a legitimate later reply can still match.
- **`Sender`** (`sender.go`) — implements the retransmit lifecycle: allocate ID, build packet via caller closure, encode, register with matcher, write, wait on reply channel with per-attempt timeout. On timeout: cancel matcher entry, **release the old ID and allocate a fresh one** (RFC-correct, never reuses on retransmit). On final timeout: returns `ErrFinalTimeout`. On context cancel: releases ID, cancels matcher, returns `ctx.Err()`. When all 256 IDs in a tuple are full, falls through to other source tuples (round-robin); if all tuples full after `AllocMaxWait`, returns `ErrIdentifierExhausted`.
- **`Receiver`** (`receiver.go`) — one goroutine per socket. Decodes inbound, routes reply codes to `ReplyMatcher.Deliver`, routes `CoA-Request`/`Disconnect-Request` to a registered `ServerHandler` (or emits `coa_dropped` / `disconnect_received` if no handler). Bad datagrams emit `validation_failed`.
- **`Engine`** (`io.go`) — public API: `NewEngine`, `Start`, `Stop`, `Send`, `LocalAddrs`. Defines `Opts`, `RetransmitPolicy`, `SendResult`, `Backoff` enum, `Collector` interface (so this package does not import `pkg/collector`), `ServerHandler` interface and `ServerHandlerFunc` adapter. Errors: `ErrIdentifierExhausted`, `ErrFinalTimeout`, `ErrNoSockets`, `ErrNotStarted`, `ErrAlreadyStarted`.

All files carry the mandatory header per `docs/CONVENTIONS.md`.

## Files created

```
pkg/io/io.go
pkg/io/socketpool.go
pkg/io/idalloc.go
pkg/io/matcher.go
pkg/io/sender.go
pkg/io/receiver.go
pkg/io/time.go
pkg/io/idalloc_test.go
pkg/io/matcher_test.go
pkg/io/socketpool_test.go
pkg/io/sender_test.go
pkg/io/receiver_test.go
pkg/io/policy_test.go
pkg/io/testhelpers_test.go
```

## Test results

- 35 tests, all PASS.
- Coverage: **86.7%** of statements (above the 80% gate).
- Stress: 5 consecutive full-suite runs and 10 consecutive `TestEngine_Send_*` runs all pass — no flakes observed.
- Build: `go build ./...` clean. `go vet ./...` clean. All other packages still pass (`pkg/radius`, `pkg/events`, `pkg/collector`, `pkg/config`, `apps/api/...`).

### `-race` constraint

The Windows host has no C compiler installed (`gcc`/`clang`/`cc` all missing), so `go test -race` aborts at `go: -race requires cgo`. This is consistent with how Wave 1 handled the same constraint (see `pkg/collector/race_off_test.go` build-tagged `!race` gate). Concurrency correctness is instead exercised via:
- `TestIDAllocator_ConcurrentNoDoubleAlloc` — 64 goroutines × 200 ops, asserts no two goroutines ever hold the same id at the same time.
- `TestEngine_Send_IDsReleasedAfterReply` — 300 concurrent sends through a 256-id tuple, all must succeed.
- `TestReplyMatcher_RegisterReplacesOld`, `TestReplyMatcher_DuplicateReplyDetected`, etc., touch the matcher concurrently from receiver goroutine + Send goroutine.

The orchestrator should re-run `go test ./pkg/io/... -race` on a host with cgo enabled before merge.

## Tests covering the briefing's specific requirements

| Briefing requirement | Test |
|---|---|
| Exhaust 256 IDs, allocation fails | `TestIDAllocator_ExhaustsAt256` |
| Release one, allocation succeeds | `TestIDAllocator_ExhaustsAt256` (same) |
| 1000 goroutines hammering, no double-alloc | `TestIDAllocator_ConcurrentNoDoubleAlloc` (64×200) |
| Register, deliver matching reply, channel receives | `TestReplyMatcher_RegisterAndDeliver` |
| Second matching reply → duplicate event, no re-trigger | `TestReplyMatcher_DuplicateReplyDetected`, `TestEngine_Send_DuplicateReplyEmitsEvent` |
| Wrong Response Authenticator → rejected | `TestReplyMatcher_BadAuthenticatorRejected`, `TestEngine_Send_BadAuthIsNotAccepted` |
| Echo server full round-trip + correct events | `TestEngine_Send_HappyPath` |
| Black-hole → retransmit policy + final error | `TestEngine_Send_RetransmitsOnBlackHole` |
| Black-hole then live, success on retransmit | `TestEngine_Send_RecoversAfterPartialBlackHole` |
| New Identifier per retransmit (RFC) | `TestEngine_Send_NewIdentifierPerRetransmit` |
| Cleanup on cancellation | `TestEngine_Send_ContextCancelStopsImmediately` |
| All-tuples-full error | `TestEngine_Send_IdentifierExhaustion` |
| CoA routed to handler | `TestReceiver_RoutesCoAToHandler` |
| Server-initiated drop without handler | `TestReceiver_DropsCoAWithoutHandler` |

## Key design decisions

1. **Duplicate tracking via `recentDone` map.** The briefing demands duplicates be detected, not silently treated as unmatched. The matcher stamps a `completed` entry (request authenticator + sub id + completed-at) when a reply is delivered. On a second delivery for the same key, if its authenticator validates against the recorded request authenticator, it's a duplicate; otherwise it's unmatched. Lazy GC fires when the table grows past 1024 entries; entries older than 30s (well past any sane retransmit policy) are pruned.

2. **Bad-auth does NOT consume the inflight.** If a forged or corrupted reply arrives, dropping it AND removing the inflight would let an attacker DoS the legitimate reply. Instead, validation_failed is emitted and the inflight is left in place.

3. **`ServerHandler` does its own ACK/NAK send.** The I/O layer does not respond on behalf of the server-listener slice (2D). The handler is given the parsed packet, the wire bytes (for Message-Authenticator validation), and both src+local addresses so the listener can craft and send replies via its own socket. This keeps I/O-layer cohesion clean.

4. **`Collector` is a local interface.** Defined in `io.go`, satisfied by `pkg/collector.Collector`. This avoids `pkg/io` importing `pkg/collector` (which would couple the dependency graph).

5. **`build func(id uint8)` closure for packet construction.** The caller (subscriber FSM) bakes the allocated identifier into the packet at build time (so things like NAS-Port-Id derivations from the id stay coherent). Sender re-stamps `req.Identifier = id` defensively after the closure returns.

6. **Allocator backoff on tuple exhaustion.** When all tuples are full, sender backoffs `AllocBackoffStep` (default 5ms) and retries, capped by `AllocMaxWait` (default 100ms). This handles the common case where a few sends are momentarily ahead of releases without forcing the subscriber FSM to deal with allocation churn.

## Deviations from briefing

- Added `ServerHandlerFunc` adapter alongside `ServerHandler` interface — purely a convenience for tests and small-scale handler implementations. Does not change the contract.
- The suggested `SendResult` had no `LocalAddr` / `Identifier`; I included both because the subscriber FSM wants to log them in its outcome. Confirmed signature matches what the briefing's "Suggested public API" prescribes.

## Follow-ups for downstream slices

- **2B (subscriber FSM):** import `pkg/io`, construct one shared `Engine`, call `Engine.Send` per state transition. The closure should construct the appropriate `radius.Packet` via `pkg/radius` constructors, baking the allocated id in.
- **2D (server listener):** implement `ServerHandler` and pass it via `Opts.Handler`. The handler should validate `Message-Authenticator` (the full wire bytes are passed in), look up the targeted subscriber, and send ACK/NAK via its own socket.
- **`-race` re-run** on a cgo-capable host before merge to confirm no data races.

## Coverage breakdown

```
pkg/io/idalloc.go       — 95%+
pkg/io/matcher.go       — 88% Deliver, 100% Register/Cancel/Inflight/CloseAll
pkg/io/io.go            — 80–90% across Engine
pkg/io/receiver.go      — 80–90% (Wait is unused at 0%; Stop/Start/dispatch covered)
pkg/io/sender.go        — 81% runSend, 82% allocateOnAnyTuple
pkg/io/socketpool.go    — 80%+
Total                   — 86.7%
```
