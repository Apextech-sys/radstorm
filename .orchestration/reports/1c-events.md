# Report 1C — Events package + collector

## What was built

- **`pkg/events`** — frozen-contract Event + SubscriberOutcome types, Category/EventType label constants, monotonic clock helper, eight typed constructors covering every event the FSM/IO/server emit.
- **`pkg/collector`** — sharded (default `runtime.NumCPU()`) collector with per-shard buffered channel (default 16384), per-shard background goroutine, periodic + batch-cap Parquet flush, and a per-shard counter set updated in-line so `Aggregate()` is a cheap reduction.
- **Parquet writers** wrapping `parquet-go/parquet-go` `GenericWriter[T]` for `Event` and `SubscriberOutcome`. File rotation on a configurable byte cap (default 256 MiB per contract). Files emitted as `events-shard-<n>-<seq>.parquet` and `subscribers.parquet`.
- **Aggregator** producing the full `Summary` per `results-schema.md`: subscribers section, establishment latency distribution + activation/establishment curve, per-subscriber retransmit distribution, CoA + Disconnect roll-ups with optional latency distributions, threshold evaluation with overall pass/fail/none rollup, artifacts pointers.
- **Percentile math** — nearest-rank (`ceil(q*N)`) implementation; verified against the briefing-mandated input `[1..1000]` → p50=500, p95=950, p99=990, p999=999, max=1000.
- **`summary.json` writer** + `summary.txt` human-readable rendering.

## Files created

- `pkg/events/category.go`
- `pkg/events/clock.go`
- `pkg/events/event.go`
- `pkg/events/outcome.go`
- `pkg/events/event_test.go`
- `pkg/collector/collector.go`
- `pkg/collector/parquet.go`
- `pkg/collector/aggregate.go`
- `pkg/collector/summary.go`
- `pkg/collector/aggregate_test.go`
- `pkg/collector/collector_test.go`
- `pkg/collector/race_off_test.go`
- `pkg/collector/race_on_test.go`
- `go.sum` (refreshed by `go mod tidy`)
- `go.mod` (added `github.com/parquet-go/parquet-go`, `github.com/stretchr/testify`)

Every file carries the mandated header block per `docs/CONVENTIONS.md`.

## Test results

```
go test ./pkg/events/... ./pkg/collector/... -v -cover -race
ok  github.com/Apextech-sys/reflex-radstorm/pkg/events     1.089s  coverage: 100.0% of statements
ok  github.com/Apextech-sys/reflex-radstorm/pkg/collector 50.408s  coverage:  86.6% of statements
```

`go build ./...` and `go vet ./...` are both clean.

Notable tests:

- `TestEventSchemaMatchesContract` / `TestSubscriberOutcomeSchemaMatchesContract` — pin the Parquet schema string. Any drift in field names, types, or order breaks the build before it can break downstream readers.
- `TestZeroLossUnderConcurrency` — 4 producers × 25k events = 100k → re-read every shard's Parquet file and assert row count == 100k. `Dropped()` is asserted 0.
- `TestPerformance1MEvents` — 8 producers × 125k events = 1M; full round-trip < 5s without the race detector (race adds 5–10x overhead so the timing assertion is gated by build tag).
- `TestParquetRoundTripPreservesFields` — submit an Event, close, re-read with `parquet.NewGenericReader[events.Event]`, assert every field round-trips (including the `Tags` map serialised as JSON).
- `TestAggregateRollupCounts` — synthetic event stream covering created/activated/terminal, retransmits, CoA Received/Acked/Naked/Dropped, Disconnect Received/Acked. Asserts every roll-up section of the Summary.
- `TestPercentileBriefingExpectations` — explicit assertion of `[1..1000]` percentile values from the briefing.

Race detector: clean (no races detected) once `mingw` was installed via `scoop install mingw` to provide a `gcc` for `cgo`.

## Schema-mapping note

The frozen contract was originally written against `xitongsys/parquet-go` tag syntax (`name=ts, type=INT64, convertedtype=TIMESTAMP_MICROS`). The briefing requires `parquet-go/parquet-go` for I/O — that library uses a different tag dialect (`ts,timestamp(microsecond)`). The Go struct tags use the parquet-go dialect; the **emitted Parquet schema** preserves the exact column names and logical types specified by the contract:

```
required int64 ts (TIMESTAMP(isAdjustedToUTC=true,unit=MICROS));   ← matches convertedtype=TIMESTAMP_MICROS
required int32 sub_id (INT(32,false));                              ← matches convertedtype=UINT_32
required int32 radius_code (INT(8,true));                           ← matches convertedtype=INT_8
required int32 identifier (INT(16,true));                           ← matches convertedtype=INT_16
required binary tags (JSON);                                        ← matches the contract note "serialized to JSON in Parquet `tags` column"
```

Two snapshot tests pin this exact schema rendering so any future struct change must be a deliberate contract amendment.

## Deviations from briefing

- **Race detector**: required, but the worktree had no C compiler. I installed `mingw` via `scoop install mingw` to satisfy `cgo` so `-race` runs. This is a session-local install; if a fresh runner doesn't have gcc, only the `-race` flag will fail — non-race tests run on bare Go.
- **Perf gate under -race**: The 1M-events-in-<5s gate is unrealistic with the race detector's overhead. The test still runs (correctness assertions + zero-loss check), but the timing assertion is suppressed via a `//go:build !race` toggle (`raceEnabled` const). With `-race` it logs the elapsed time instead of failing.
- **`artifacts.events_parquet`** in summary.json is `events-shard-*.parquet` (a glob) rather than a single filename — the collector writes one file per shard by design (per the contract's "Shard receivers each own their own Parquet writer to avoid contention"). The analyse-results CLI is expected to glob.

## Follow-ups for downstream waves

- Wave 2 subscriber/IO/server slices should call `events.NewRequestSent`, `NewReplyReceived`, etc. — the constructors are the canonical emitter API.
- Wave 3 scenario driver wires up `collector.New(...)` early and calls `Stop`+`Aggregate`+`WriteSummary` at end-of-test.
- Wave 3 API/frontend reads `summary.json` directly; the `Summary` struct is the wire shape.
- The `ServerHealth.UnresponsivePeriods` field is currently always `[]` — populating it is a Wave 2 server-listener concern (it owns the health probe).
- The CLI command `analyze-results <dir>` (per `docs/ARCHITECTURE.md`) needs to glob `events-shard-*.parquet` and `subscribers.parquet`.

## Commit

`feat(events,collector): shared event schema, sharded collector with Parquet output` on branch `wave-1/1c-events`. Not pushed.
