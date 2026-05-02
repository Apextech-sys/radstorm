# Wave 2 Slice 2B — Subscriber FSM (implementation report)

## Outcome

Done. Package `pkg/subscriber` implemented per briefing
`.orchestration/briefings/2b-subscriber.md`.

- `go test ./pkg/subscriber/... -cover -v` — **PASS** (29 tests, **91.6%** coverage)
- `go build ./...` — clean

`-race` could not run on this worktree (CGO/gcc unavailable on the
Windows host); same constraint observed by Wave 1 slices. Concurrent
tests are nonetheless authored to be useful under `-race` when CI
provides a CGO toolchain (atomic counters, mutex-guarded writes,
hammer-style goroutine fan-out).

## Files added

- `pkg/subscriber/state.go` — State enum + classifiers (`IsTerminal`, `IsFailure`)
- `pkg/subscriber/sender.go` — `Sender` interface, `RetransmitPolicy`,
  `SendResult` (the duck-typed contract pkg/io.Engine.Send must
  satisfy in slice 2A)
- `pkg/subscriber/subscriber.go` — `Subscriber` struct + `Run` lifecycle
  + `OnCoA`/`OnDisconnect` inbound handlers
- `pkg/subscriber/pool.go` — `Pool` with O(1) indexes by id, username,
  acct-session-id, framed-ip-address; `SubscriberTarget` interface;
  `Pool.Lookup` priority dispatch
- `pkg/subscriber/lookup.go` — `BuildLookup(*Pool) LookupFunc` adapter
  for the server listener (slice 2D)
- `pkg/subscriber/fakes_test.go` — `fakeSender` (scripted), `fakeCollector`
  (recording)
- `pkg/subscriber/subscriber_test.go` — happy paths (PAP, CHAP,
  with/without acct), every failure path, identity invariants
- `pkg/subscriber/pool_test.go` — index integrity, recycled-credentials
  uniqueness, lookup priority order, BuildLookup wiring
- `pkg/subscriber/concurrent_test.go` — concurrent OnCoA hammer,
  idempotent OnDisconnect under contention, parallel pool reads

## State machine

Implemented exactly per spec §2.3:

```
idle ─activate─► auth_sent ─Access-Accept─► (acct_sent ─Accounting-Response─► established)
                  │                                │
                  ├─ Access-Reject ──► auth_failed │
                  └─ timeout ────────► auth_failed └─ timeout ──► acct_failed
                                                    └─ unexpected ──► acct_failed
established ─Disconnect-Request─► terminated
```

State string values match `pkg/events.FinalState*` constants and
appear verbatim in the Parquet `state` and `final_state` columns.

## Contracts honoured

- **Event schema** (`.orchestration/contracts/event-schema.md`):
  every transition emits a `state_changed`; every reply emits
  `reply_received`; every terminal exit emits `terminal` +
  `SubmitOutcome`.
- **Results schema** (`.orchestration/contracts/results-schema.md`):
  `SubscriberOutcome` populated with `-1` sentinels when an
  establishment milestone never occurred (`EstablishedAtOffsetMs`,
  `EstablishmentLatencyMs`, `DisconnectReceivedAt`).
- **Sender contract** (coordination point with slice 2A):
  ```go
  type Sender interface {
      Send(ctx context.Context, dst net.Addr,
           build func(id uint8) (*radius.Packet, error),
           policy RetransmitPolicy, subID uint32,
      ) (*SendResult, error)
  }
  ```
  The `build` closure model means each retransmit gets a fresh
  Identifier (per `docs/PROTOCOL.md`) without the FSM keeping state
  across retries.
- **SubscriberTarget** for the server listener (slice 2D): duck-typed,
  no shared interface package, no import cycle.

## Test design notes

- `fakeSender` records every recorded call so tests can assert the
  packet the FSM emitted (PAP vs CHAP attribute presence, acct
  status-type, etc.).
- `stepClock` and `fixedClock` helpers allow deterministic offset and
  latency assertions.
- The retransmit count from `SendResult.Retransmits` is propagated
  into `SubscriberOutcome.AuthRetransmits` / `AcctRetransmits` so
  `analyze-results` and the frontend dashboard see a faithful retry
  distribution.

## Known follow-ups (for later waves, not this slice)

- CoA inbound handling could update session state in response to
  Huawei rate-plan VSAs — currently we just bump the counter and
  emit an event (per briefing scope).
- The `Sender` interface return value uses `(*SendResult, error)`.
  Slice 2A's pkg/io.Engine.Send must mirror this; if the integration
  team prefers a single-return shape, both packages migrate together.

## Coverage breakdown (informal)

- state.go — 100% (every State + classifier exercised)
- sender.go — 100% (PolicyFromConfig defaults + SendResult.Latency edges)
- subscriber.go — ~90% (one hard-to-hit path: address-resolution failure
  with a syntactically valid string is environment-dependent on
  Windows; covered indirectly via the missing-deps tests)
- pool.go — 100%
- lookup.go — 100%
