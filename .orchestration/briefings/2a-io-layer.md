# Briefing 2A — I/O Layer

## Mission

Build `pkg/io` — the UDP socket pool, RADIUS Identifier allocator, reply matcher, sender (with retransmit), and receiver. This is the network plumbing that the subscriber FSM (slice 2B) and the server listener (slice 2C) sit on top of.

## Context — read these first

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md` — especially "The hard parts" section (ID exhaustion, retransmit semantics, goroutine model)
3. `docs/PROTOCOL.md` — RADIUS Identifier rules
4. `docs/CONVENTIONS.md` — file headers MANDATORY
5. **`.orchestration/contracts/event-schema.md`** — emit Events through the collector for every meaningful action
6. `pkg/radius/*.go` — your dependency. Read its public API. Specifically `Packet`, `Encode`, `Decode`, the constructors.
7. `pkg/events/*.go` — Event type, helper constructors
8. `pkg/collector/*.go` — `Collector.Submit(Event)` is your sink

## Working directory

- **Worktree:** `C:\dev\radstorm-2a` on branch `wave-2/2a-io`
- **Files you own:**
  - `pkg/io/socketpool.go` — bind UDP sockets across configured source IPs
  - `pkg/io/idalloc.go` — per-tuple 8-bit Identifier bitmap
  - `pkg/io/matcher.go` — outstanding-request correlation table; matches replies; detects duplicates
  - `pkg/io/sender.go` — sends a Packet, manages retransmit lifecycle
  - `pkg/io/receiver.go` — parses inbound, routes replies to matcher and server-initiated to a callback
  - `pkg/io/io.go` — the public Engine that ties them together
  - `pkg/io/*_test.go`

## Scope

**In scope:**
- `SocketPool`: given a list of source IPs and a port range, bind UDP sockets on each. Uses `net.ListenUDP`. Round-robin or random selection of which (srcIP, srcPort) to use for a new request.
- `IDAllocator`: per-`(srcIP, srcPort, dstIP, dstPort)` tuple, maintain a 256-bit bitmap. Methods: `Allocate(tuple) (id uint8, ok bool)` (non-blocking, returns false if full), `Release(tuple, id)`. Thread-safe.
- `ReplyMatcher`: indexed by `(localAddr, identifier)`. When a reply arrives, look up the outstanding request, verify Response Authenticator matches, deliver to the waiting goroutine via a channel. If a duplicate reply arrives (matching identifier but the request has already received its reply), log via Event submission as `duplicate_reply` and discard.
- `Sender.Send(ctx, request, retransmitPolicy)` returns `(reply *Packet, err error)`:
  - allocate ID from allocator (block briefly with backoff if all 256 in flight for this tuple; fail with clear error after some bound)
  - register with matcher
  - serialize via `radius.Encode`, write to socket
  - emit `request_sent` event
  - wait on matcher's reply channel with timeout from policy
  - on timeout: emit `request_retransmitted`, allocate a NEW id (RFC-correct), reschedule per policy
  - on final timeout: release id, return error
  - on reply: release id, emit `reply_received` with latency, return Packet
- `Receiver`: per socket goroutine reads inbound. Decodes via `radius.Decode`. If code is reply (Access-Accept/Reject, Accounting-Response): route to matcher. If server-initiated (CoA-Request, Disconnect-Request): route to a registered `ServerHandler` interface (the server listener registers itself).
- `ServerHandler interface { HandleServerInitiated(p *radius.Packet, srcAddr net.Addr) }` — defined here, implemented later by `pkg/server`.
- All operations log Events through a `Collector` interface defined locally so we don't import `pkg/collector` directly:
  ```go
  type Collector interface {
      Submit(events.Event)
  }
  ```

**Out of scope:**
- Subscriber state (2B)
- CoA validation/ACK (2C — server listener)
- Scenario lifecycle (Wave 3)

## Suggested public API

```go
package io

type Engine struct{ /* ... */ }

type Opts struct {
    SourceIPs   []net.IP
    PortRangeLo int
    PortRangeHi int
    Collector   Collector
    Handler     ServerHandler   // for inbound CoA/Disconnect; nil = drop
}

func NewEngine(opts Opts) (*Engine, error)
func (e *Engine) Start(ctx context.Context) error
func (e *Engine) Stop(ctx context.Context) error

type RetransmitPolicy struct {
    InitialTimeout time.Duration
    MaxRetries     int
    Backoff        Backoff   // Exponential | Linear | Constant
    BackoffBase    time.Duration
}

type SendResult struct {
    Reply        *radius.Packet
    LatencyUs    int64
    RetransmitN  int
    LocalAddr    net.Addr
    Identifier   uint8
}

func (e *Engine) Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy RetransmitPolicy, subID uint32) (*SendResult, error)

// (We pass build func(id) so caller can construct the packet with the allocated identifier)
```

## Critical correctness requirements

- **New Identifier per retransmit.** Reusing the same ID would race with a possibly-late original reply.
- **Duplicate detection.** If two replies arrive for the same originally-sent (id, request_authenticator), match the FIRST and treat the SECOND as duplicate — emit event but do not re-trigger.
- **Response Authenticator validation on every reply.** Bad authenticator = drop + emit event.
- **Cleanup on cancellation.** ctx done → release IDs, close pending channels.
- **Block sensibly when ID space exhausted.** Default: try to allocate from another (srcIP, srcPort) tuple; if ALL tuples full for this (dstIP, dstPort), return error so caller can decide (subscriber will fail).

## Success criteria

- `go build ./...` and `go test ./pkg/io/... -race -cover` pass
- ≥80% coverage
- Specific tests:
  - IDAllocator: exhaust 256 IDs, verify allocation fails; release one, verify allocation succeeds
  - IDAllocator: 1000 goroutines hammering allocate/release, no double-allocation (use go test -race)
  - ReplyMatcher: register a request, deliver matching reply, verify channel receives it
  - ReplyMatcher: register a request, deliver matching reply, deliver second matching reply, verify second is reported as duplicate (count via Event submission to a fake Collector)
  - ReplyMatcher: deliver a reply with wrong Response Authenticator, verify it's rejected
  - Sender: against an in-process echo server (helper), full round-trip works and emits the right Events
  - Sender: against a black-hole server (sink that never replies), retransmit policy fires correctly, final error after MaxRetries
  - Sender: black-hole then live: server starts replying after 2 timeouts; verify success on retransmit 3

## Test infrastructure

Use a small in-process UDP echo helper that decodes the request, builds an Access-Accept response with proper Response Authenticator, and writes it back. This avoids needing the Docker rig for unit tests.

## File-header requirement

Every Go file gets the header per `docs/CONVENTIONS.md`.

## Reporting

Write `.orchestration/reports/2a-io-layer.md` per template.

## When you finish

1. `export PATH="$PATH:/c/Program Files/Go/bin" && go test ./pkg/io/... -race -cover -v`
2. `go build ./...`
3. Write report
4. Commit on `wave-2/2a-io` (do NOT push, do NOT merge)
5. Return concise (<200 word) summary
