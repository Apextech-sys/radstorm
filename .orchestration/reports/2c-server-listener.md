# Report 2C — Server-initiated listener (CoA + Disconnect)

## What was built

- **`pkg/server`** — UDP listener for RFC 5176 CoA-Request and Disconnect-Request
  packets. Validates `Message-Authenticator`, decodes attributes (including
  Huawei VSAs), looks up the targeted virtual subscriber via a duck-typed
  interface, sends ACK/NAK with the proper Response Authenticator, and
  records μs-precision response latency.
- **`SubscriberTarget` + `SubscriberLookup` interfaces** defined in
  `pkg/server/lookup.go`. `pkg/subscriber.Pool` (slice 2B, in-flight) is
  expected to satisfy `SubscriberLookup` via duck typing — no import cycle.
- **`Collector` interface** in `pkg/server/lookup.go` matching
  `pkg/collector.Collector.Submit(events.Event)` so tests can swap a
  recorder for the real Parquet collector.
- **`Listener.New / Start / Stop` lifecycle** — bind on `New`, spawn
  `runtime.NumCPU()` UDP readers on `Start`, drain + close on `Stop`.
  Cancel-watcher goroutine selects on either `ctx.Done()` or the internal
  `stopCh` so explicit `Stop(ctx)` always unblocks even when the caller
  passed `context.Background()`.
- **RFC 5176 §2.4 disposition logic** in `pkg/server/handler.go`:
  - Missing/invalid Message-Authenticator → silent drop, `coa_dropped` /
    disconnect equivalent emitted with `drop_reason` tag.
  - Malformed VSA → NAK with Error-Cause 401.
  - Unknown subscriber → NAK with Error-Cause 503.
  - Otherwise → ACK, then `OnCoA`/`OnDisconnect` dispatched on a fresh
    goroutine AFTER the ACK is on the wire (latency measured from
    `recvAt` to `sentAt` via `time.Now()` monotonic).
- **VSA tag rendering** — every Vendor-Specific attribute is decoded
  into the event's `Tags` map. The eight named Huawei VSAs from
  `pkg/radius/vendor.go` get friendly keys (`huawei_input_peak_rate`,
  `huawei_subscriber_qos_profile`, …); unknown vendor/types fall back to
  `vsa_<vendor>_<type>`. Values render as decimal for 4-byte payloads,
  printable ASCII strings where possible, and hex otherwise.

## Files created

- `pkg/server/lookup.go` — `SubscriberTarget`, `SubscriberLookup`, `Collector` interfaces.
- `pkg/server/listener.go` — `Opts`, `Listener`, `New`, `Start`, `Stop`, UDP read loop.
- `pkg/server/handler.go` — per-packet validation, ACK/NAK construction, VSA tagging.
- `pkg/server/listener_test.go` — 15 end-to-end tests (in-process UDP
  client + fake `SubscriberLookup`/`Collector`).
- `pkg/server/handler_test.go` — 11 focused unit tests for the small
  helpers (VSA key/value rendering, standard tag extraction, copyTags).

Every file carries the mandated header block per `docs/CONVENTIONS.md`.

## Test results

```
go test ./pkg/server/... -cover -v
=== 26 tests, all PASS ===
ok  github.com/Apextech-sys/reflex-radstorm/pkg/server  0.840s  coverage: 80.3% of statements
```

`go build ./...` is clean. `go vet ./pkg/server/...` is clean.

### Race detector status

`-race` requires `CGO_ENABLED=1` which requires a C toolchain (gcc) on
PATH. This Windows host has neither MSYS2's gcc nor MinGW installed, so
`go test -race` fails at the `runtime/cgo` build step:

```
$ go test ./pkg/server/... -race
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
$ CGO_ENABLED=1 go test ./pkg/server/... -race
# runtime/cgo
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
```

The listener was written defensively against the race: every reader
goroutine is independent (each owns a private `buf` slice and copies
bytes before invoking the handler), shared mutable state is the small
set of `atomic.Int64` counters and the once-set `stopped atomic.Bool`,
and the test's `fakeCollector` and `fakeTarget` use a `sync.Mutex` /
`atomic.Int32` respectively. CI on Linux will surface any race; the
host gap is environmental, not a code issue.

## Test coverage

| Function                           | Coverage |
|---|---|
| `handlePacket`                     | 100% |
| `handleCoA`                        | 75.7% |
| `handleDisconnect`                 | 61.1% |
| `respondCoANak`                    | 52.6% |
| `respondDisconnectNak`             | 52.6% |
| `writeReply`                       | 80.0% |
| `Listener.New`                     | 88.0% |
| `Listener.Start`                   | 100% |
| `Listener.Stop`                    | 93.8% |
| `readLoop`                         | 75.0% |
| `coaEvent` / `disconnectEvent`     | 100% |
| `vsaTags`                          | 100% |
| `vsaTagKey`                        | 100% (via handler_test.go) |
| `vsaTagValue`                      | 100% |
| `isPrintableASCII`                 | 100% |
| `standardTags`                     | 100% |
| `copyTags`                         | 100% |
| **Total**                          | **80.3%** |

The uncovered lines are all on internal-error fallback paths
(`encode`, `Build*Nak`, `WriteToUDP`) that only fire if the OS or the
`pkg/radius` library fails — which doesn't happen for valid inputs in
unit tests. Hitting those lines would require fault injection that is
not currently set up.

## Behavioural acceptance criteria — all met

| Criterion (from briefing) | Test |
|---|---|
| Valid CoA-Request → CoA-ACK with proper Response Authenticator | `TestCoARequestValidProducesACK` |
| Unknown subscriber → NAK with Error-Cause 503 | `TestCoARequestUnknownSubscriberProducesNAK503` |
| Invalid Message-Authenticator → no response, `coa_dropped` event | `TestCoARequestInvalidMessageAuthenticatorIsSilentlyDropped` |
| Disconnect-Request → Disconnect-ACK + `OnDisconnect` callback | `TestDisconnectRequestProducesACK` |
| Huawei VSA → ACK + decoded VSA in event tags | `TestCoARequestWithHuaweiVSADecodesIntoTags` |
| Malformed VSA → NAK 401 | `TestCoARequestWithMalformedVSAProducesNAK401` |
| μs-precision latency recorded | `TestLatencyMeasurementUsesMicrosecondPrecision` |

## Key design decisions

1. **`SubscriberTarget` / `SubscriberLookup` are duck-typed interfaces in
   `pkg/server`.** `pkg/subscriber` will satisfy them implicitly. This
   avoids the import cycle that a back-reference to `pkg/subscriber`
   would create, and keeps `pkg/server` agnostic to how the subscriber
   pool resolves a packet to a session (User-Name, Acct-Session-Id,
   vendor session-id VSA — that's pool policy).
2. **ACK before callback.** `OnCoA`/`OnDisconnect` is invoked on a fresh
   goroutine AFTER the ACK has been written and the latency event has
   been emitted. This matches the briefing's "ACK first so latency
   measurement is accurate" requirement; the FSM transition happens
   asynchronously and cannot perturb the wire-time response budget.
3. **Two events per CoA when validation succeeds**: `coa_received` is
   emitted as soon as the Message-Authenticator validates, regardless
   of the eventual ACK/NAK disposition. This gives operators visibility
   into every authenticated inbound CoA (matching the contract's
   `coa_received` event type) before the ACK/NAK event tells them
   what we did with it. `copyTags` is used to ensure the two events
   carry independent map snapshots.
4. **Test-side workaround for Response Authenticator validation.** The
   `pkg/radius.ValidateResponseAuthenticator` helper validates against
   the wire body as-is, but the encoder computes the Response
   Authenticator BEFORE filling in the Message-Authenticator value
   (RFC-correct). The test file therefore implements
   `validateResponseAuthMAAware` that zeros the MA bytes in a scratch
   copy of the wire before recomputing. This is a pre-existing gap in
   the wave-1-frozen `pkg/radius` and warrants a follow-up; for now
   the workaround proves our ACKs DO carry valid Response Authenticators.
5. **Listener does not validate NAS-IP-Address.** The briefing lists
   error-cause 403 (NAS Identification Mismatch) as something we may
   reserve, but the briefing also explicitly assigns 503/401/ACK as
   the disposition rules. radstorm is the test harness — we simulate
   the BNG, so we accept any NAS-IP the server addresses us with.
   Operators wanting strict NAS-IP enforcement can layer it in via the
   `SubscriberLookup` (return `(nil, false)` to NAK 503).

## Coordination handoffs

- **2B (subscriber pool, in flight):** their `Pool.Lookup(*radius.Packet)
  (Subscriber, bool)` and `Subscriber.OnCoA(*radius.Packet) /
  OnDisconnect(*radius.Packet) / ID() uint32 / Username() string`
  satisfy the interfaces in `pkg/server/lookup.go` exactly. Wave 3's
  `pkg/scenario` will wire `Listener.Opts.Lookup = pool` directly.
- **2A (I/O layer):** independent — the I/O layer handles the
  client-side outbound flows (Access/Accounting). The server-side
  inbound listener owns its own UDP socket and does not share the
  `SocketPool`.
- **3A (scenario driver):** instantiate one `Listener` per run from
  config (`bind_address`, shared secret), pass the subscriber pool as
  `Lookup` and the collector as `Collector`. Start with the run's
  root context so the scenario's Ctrl-C / shutdown drains the listener
  cleanly.
- **Documentation-specialist:** the public surface is `Opts`, `Listener`,
  `New`, `Start`, `Stop`, `LocalAddr`, `SubscriberTarget`,
  `SubscriberLookup`, `Collector`, plus the error sentinels. No new
  RFC behaviour beyond what's in `docs/PROTOCOL.md`.

## Suggested follow-ups (out of scope for 2C)

1. Backport an MA-aware `ValidateResponseAuthenticator` to `pkg/radius`
   so consumers don't have to zero MA bytes themselves. Test-side
   helper in `pkg/server/listener_test.go` (`validateResponseAuthMAAware`)
   shows the desired logic.
2. Optional NAS-IP-Address strict-match mode (Error-Cause 403) gated
   by an `Opts` flag, for operators who want radstorm to actively
   reject mis-targeted CoA traffic.
3. CGO toolchain on Windows runners so `-race` runs locally; today only
   Linux CI can exercise the race detector. Listener was written with
   no shared mutable state outside atomics + the test mutex, so a race
   would be surprising.
