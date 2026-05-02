# Briefing 2C — Server-initiated listener (CoA + Disconnect)

## Mission

Build `pkg/server` — the listener that handles RFC 5176 CoA-Request and Disconnect-Request packets initiated by the RADIUS server under test. Validates Message-Authenticator, decodes attributes (including Huawei VSAs), looks up the targeted subscriber, sends ACK/NAK with proper auth, records latency.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md`
3. `docs/PROTOCOL.md` — especially CoA error-cause codes (RFC 5176)
4. `docs/CONVENTIONS.md` — file headers MANDATORY
5. **`.orchestration/contracts/event-schema.md`**
6. `pkg/radius/` — uses constructors `NewCoAAck/NewCoANak/NewDisconnectAck/NewDisconnectNak` and `Packet.ValidateMessageAuthenticator`
7. `pkg/events/`, `pkg/collector/`

## Working directory

- **Worktree:** `C:\dev\radstorm-2c` on branch `wave-2/2c-server-listener`
- **Files you own:**
  - `pkg/server/listener.go` — UDP listen, packet routing
  - `pkg/server/handler.go` — CoA + Disconnect handler logic
  - `pkg/server/lookup.go` — defines a small `SubscriberLookup` interface that pkg/subscriber's Pool implements (decouples)
  - `pkg/server/*_test.go`

## Scope

**In scope:**
- `SubscriberTarget` interface — the server listener calls these methods on whatever the lookup returns:
  ```go
  type SubscriberTarget interface {
      ID() uint32
      Username() string
      OnCoA(*radius.Packet)
      OnDisconnect(*radius.Packet)
  }
  type SubscriberLookup interface {
      Lookup(*radius.Packet) (SubscriberTarget, bool)
  }
  ```
  pkg/subscriber's Pool will satisfy `SubscriberLookup` and Subscriber satisfies `SubscriberTarget`.
- `Listener`:
  - Binds UDP `bind_address` from config
  - Spawns N goroutines reading inbound (N = runtime.NumCPU())
  - For each packet: decode via radius package; verify code is CoA-Request (43) or Disconnect-Request (40); validate Message-Authenticator (silently drop if invalid per RFC); look up subscriber via `SubscriberLookup`; if not found → NAK with error-cause 503; else → ACK and dispatch to subscriber's `OnCoA`/`OnDisconnect`. Send the response within ~1ms (asynchronously call `OnCoA` after sending response so latency measurement is accurate).
  - Emits Events: `coa_received`, `coa_acked`, `coa_naked`, `coa_dropped` (and disconnect equivalents). Records latency from receive→ack via monotonic clock.
- `Listener.Start(ctx) error` / `Listener.Stop(ctx) error`
- Public API:
  ```go
  type Opts struct {
      BindAddress  string
      SharedSecret []byte
      Lookup       SubscriberLookup
      Collector    Collector
  }
  func New(opts Opts) (*Listener, error)
  func (l *Listener) Start(ctx context.Context) error
  func (l *Listener) Stop(ctx context.Context) error
  ```
- CoA validation rules per spec §2.4:
  - Unknown subscriber → NAK 503
  - Missing/invalid Message-Authenticator → drop (silently, per RFC), log via event as `coa_dropped`
  - Malformed VSAs → NAK 401
  - Otherwise → ACK
- Decode common Huawei VSAs into a structured representation in event tags so we can verify in tests we received the right CoA payload.

**Out of scope:**
- Initiating CoA (operator does this from RADIUS server side; we just receive)
- The actual subscriber FSM transitions (delegated to `OnCoA`/`OnDisconnect`)

## Success criteria

- `go test ./pkg/server/... -race -cover` passes with ≥80% coverage
- Specific tests using a fake SubscriberLookup + in-process radclient-style sender:
  - Send a valid CoA-Request → receive CoA-ACK with proper Response Authenticator
  - Send a CoA-Request for unknown subscriber → receive NAK with Error-Cause=503
  - Send a CoA-Request with invalid Message-Authenticator → no response (timeout), `coa_dropped` event recorded
  - Send a Disconnect-Request → receive Disconnect-ACK and lookup's OnDisconnect was called
  - Send a CoA with a Huawei VSA → ACK and the VSA shows up in the recorded event tags
  - Latency event records μs-precision timestamp delta

## File-header requirement

Mandatory.

## When you finish

1. `export PATH="$PATH:/c/Program Files/Go/bin" && go test ./pkg/server/... -race -cover -v`
2. `go build ./...`
3. Write `.orchestration/reports/2c-server-listener.md`
4. Commit on `wave-2/2c-server-listener`. Do NOT push.
5. <200-word summary.
