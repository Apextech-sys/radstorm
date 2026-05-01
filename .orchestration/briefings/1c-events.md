# Briefing 1C — Events package + collector skeleton

## Mission

Build `pkg/events` (the shared Event type + Parquet schema) and `pkg/collector` (the sharded collector with Parquet writers and summary aggregation). Many other components depend on the Event type — get the shape right.

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/CONVENTIONS.md`
3. **`.orchestration/contracts/event-schema.md`** — frozen
4. **`.orchestration/contracts/results-schema.md`** — frozen
5. `docs/ARCHITECTURE.md`

## Working directory

- **Worktree:** `C:\dev\radstorm` on `main` (no parallel writers to `pkg/events/` or `pkg/collector/`)
- **Files you own:**
  - `pkg/events/event.go` — Event struct, EventType constants, helper constructors
  - `pkg/events/outcome.go` — SubscriberOutcome struct
  - `pkg/events/category.go` — Category constants
  - `pkg/events/*_test.go`
  - `pkg/collector/collector.go` — sharded channels, ring buffer, lifecycle
  - `pkg/collector/parquet.go` — Parquet writer per shard
  - `pkg/collector/aggregate.go` — end-of-test summary aggregation per `results-schema.md`
  - `pkg/collector/summary.go` — Summary struct mirroring summary.json
  - `pkg/collector/*_test.go`

## Scope

**In scope:**
- Event struct exactly per contract (with parquet struct tags)
- Constants for Category and EventType (e.g. `CategorySubscriberLifecycle = "subscriber_lifecycle"`, `EventTypeRequestSent = "request_sent"`)
- Helper constructors: `NewSubscriberCreated(...)`, `NewRequestSent(...)`, `NewReplyReceived(...)`, etc.
- Sharded Collector: `New(shardCount int, dir string, flushInterval time.Duration) *Collector`
- `Submit(e Event)` — non-blocking, picks shard by `(e.SubscriberID % shardCount)`
- Background goroutines per shard read events and append to in-memory buffer
- Periodic flush to `<dir>/events-shard-<n>-<seq>.parquet`
- `Stop(ctx)` — graceful shutdown: stop accepting, drain channels, final flush
- Aggregate function: reads all shards, plus a separate stream for SubscriberOutcomes, computes the full Summary per results-schema.md
- Helpers: percentile computation (`p50`, `p95`, `p99`, `p999`, `max`)
- Write `summary.json`, `summary.txt` (human-readable), at end of test

**Out of scope:**
- The actual emission of events (that happens from subscriber/io/server in later slices — they just call `Submit(...)`)
- Parquet reading (that's the analyze-results CLI command)

## Suggested API

```go
package collector

type Collector struct {
    // ...
}

func New(opts Opts) (*Collector, error)
type Opts struct {
    Dir              string
    ShardCount       int             // default runtime.NumCPU()
    PerShardBuffer   int             // default 16384
    FlushInterval    time.Duration   // default 30s
}

func (c *Collector) Submit(e events.Event)
func (c *Collector) SubmitOutcome(o events.SubscriberOutcome)
func (c *Collector) Stop(ctx context.Context) error
func (c *Collector) Aggregate(thresholds []Threshold) (*Summary, error)

type Summary struct { /* mirrors summary.json structure */ }
```

## Success criteria

- `go test ./pkg/events/... ./pkg/collector/...` passes with ≥80% coverage
- Specific tests:
  - Submit 100k events from 4 goroutines, Stop, verify zero loss (count file rows)
  - Aggregation produces a Summary with correct counts from a synthetic event stream
  - Percentile math: known input `[1..1000]` → p50≈500, p99≈990, p999≈999
  - Parquet files are valid (open with `github.com/parquet-go/parquet-go` reader, verify schema)
  - summary.json round-trips through JSON marshal/unmarshal

## Test requirements

- Performance-ish test (not a benchmark gate): submitting 1M events from 8 goroutines takes <5s and produces correct count

## File-header requirement

Mandatory per CONVENTIONS.md.

## Dependencies you may add

- `github.com/parquet-go/parquet-go`
- `github.com/stretchr/testify`

## Reporting

Write `.orchestration/reports/1c-events.md` per template.
