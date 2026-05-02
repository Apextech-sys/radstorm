# Briefing 2B — Subscriber state machine

## Mission

Build `pkg/subscriber` — the per-virtual-subscriber finite state machine that drives one subscriber through the full Access-Request → Access-Accept → Accounting-Request → Accounting-Response flow, plus inbound CoA/Disconnect handling. This is the heart of the test engine.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md` — subscriber boundary
3. `docs/PROTOCOL.md`
4. `docs/CONVENTIONS.md` — file headers MANDATORY
5. **`.orchestration/contracts/event-schema.md`** — emit Events for every state change
6. **`.orchestration/contracts/results-schema.md`** — your `SubscriberOutcome` field is the per-row in `subscribers.parquet`
7. `pkg/radius/` — protocol (already merged)
8. `pkg/config/` — Config + Credential (already merged)
9. `pkg/events/` — Event types (already merged)
10. `pkg/collector/` — `Collector.Submit` is your event sink

## Working directory

- **Worktree:** `C:\dev\radstorm-2b` on branch `wave-2/2b-subscriber`
- **Files you own:**
  - `pkg/subscriber/state.go` — State enum, Outcome struct alias
  - `pkg/subscriber/subscriber.go` — Subscriber struct, lifecycle methods
  - `pkg/subscriber/pool.go` — Pool: manages a slice of subscribers, indexes for lookup (by ID, by Username, by Acct-Session-Id, by Framed-IP-Address)
  - `pkg/subscriber/lookup.go` — implements `pkg/io.ServerHandler`-compatible lookup interface for inbound CoA/Disconnect routing
  - `pkg/subscriber/*_test.go`

## Scope

**In scope:**
- States exactly per spec §2.3 (also documented in ARCHITECTURE.md):
  - `idle | auth_sent | auth_retry | auth_failed | acct_sent | acct_retry | acct_failed | established | terminated`
- `Subscriber` struct holds: id, credential, NAS attributes, sessionID, frameIP, current state, timing records, retransmit counts
- `Subscriber.Run(ctx, deps)` is the lifecycle:
  1. Set state `auth_sent`, build Access-Request via `radius.NewAccessRequestPAP/CHAP` with the standard attribute set, call `deps.Sender.Send(...)`
  2. On Access-Accept: if `IncludeAcctStart` is true, transition to `acct_sent`, send Accounting-Request (Acct-Status-Type = Start), wait for Accounting-Response. On success, transition to `established` and emit a SubscriberOutcome (via collector). On Access-Reject: `auth_failed` + outcome emitted. On send error after retries: `auth_failed`/`acct_failed`.
- `deps` is an interface; the test substitutes a fake sender. Defined locally:
  ```go
  type Deps struct {
      Sender    Sender
      Collector Collector
      Config    *config.Config       // for retransmit policy, NAS attrs, etc.
      Now       func() time.Time     // injectable for test determinism
  }
  type Sender interface {
      Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy io.RetransmitPolicy, subID uint32) (*io.SendResult, error)
  }
  type Collector interface {
      Submit(events.Event)
      SubmitOutcome(events.SubscriberOutcome)
  }
  ```
- `Pool`:
  ```go
  type Pool struct{}
  func NewPool(creds []config.Credential, cfg *config.Config) *Pool
  func (p *Pool) Get(id uint32) *Subscriber
  func (p *Pool) LookupByUsername(u string) *Subscriber
  func (p *Pool) LookupBySessionID(s string) *Subscriber
  func (p *Pool) LookupByFramedIP(ip string) *Subscriber
  func (p *Pool) Range(fn func(*Subscriber))
  ```
- `lookup.go` exposes a function `BuildLookup(p *Pool) func(p *radius.Packet) *Subscriber` that the server listener (2C) consumes — looks up by Acct-Session-Id, then User-Name, then Framed-IP-Address.
- Inbound handlers (called by server listener, NOT by Subscriber.Run):
  - `Subscriber.OnCoA(p *radius.Packet)` — emit event, leave state in `established`
  - `Subscriber.OnDisconnect(p *radius.Packet)` — transition to `terminated`, emit outcome update

**Out of scope:**
- Activation scheduling (that's Wave 3 scenario driver)
- Server listener itself (2C)
- I/O layer details (consume the interface)

## Critical correctness

- The state machine MUST ALWAYS emit events per the contract — every state transition logs an event with the new state name
- A subscriber that terminates (success or failure) MUST emit a SubscriberOutcome via `Collector.SubmitOutcome`
- Test deterministically with injected `Now` function and fake Sender

## Success criteria

- `go test ./pkg/subscriber/... -race -cover` passes with ≥85% coverage
- Specific tests:
  - Happy path PAP: with fake Sender that returns Access-Accept then Accounting-Response, subscriber reaches `established` and outcome is emitted with the right latency
  - Happy path CHAP: same with CHAP construction
  - Auth failed (server returns Access-Reject): subscriber ends in `auth_failed`
  - Auth timeout (sender returns timeout error): `auth_failed`, retransmit count from sender carried into outcome
  - Acct failed: Access-Accept arrives but Acct-Response times out: `acct_failed`
  - Pool lookup by Username, SessionID, Framed-IP — all work
  - OnDisconnect transitions established → terminated and emits outcome
  - OnCoA leaves state unchanged, emits event

## File-header requirement

Mandatory.

## When you finish

1. `export PATH="$PATH:/c/Program Files/Go/bin" && go test ./pkg/subscriber/... -race -cover -v`
2. `go build ./...`
3. Write `.orchestration/reports/2b-subscriber.md`
4. Commit on `wave-2/2b-subscriber`. Do NOT push.
5. Return <200 word summary.
