// Package collector — sharded event collector with periodic Parquet flush.
//
// Purpose:
//
//	Receives Event records from every component of the harness on a set
//	of buffered channels (one per CPU core, hash by SubscriberID), writes
//	them to per-shard Parquet files via a background goroutine per shard,
//	and aggregates an end-of-test Summary per the frozen results-schema.md
//	contract.
//
// Related files:
//   - pkg/collector/parquet.go (per-shard Parquet writer)
//   - pkg/collector/aggregate.go (end-of-test summary roll-ups)
//   - pkg/collector/summary.go (output JSON shape)
//   - pkg/events/event.go (input record type)
//   - .orchestration/contracts/event-schema.md (sharding + flush guidance)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: Public — Collector is the harness-wide event sink. The Submit
// API is hot-path-critical and intentionally non-blocking. Stop(ctx) is
// the canonical drain-and-flush shutdown signal.
package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
)

// Opts configures a Collector. Zero values are replaced with sane
// defaults documented next to each field.
type Opts struct {
	// Dir is the directory Parquet files are written to. Created with
	// 0755 if absent. Required.
	Dir string

	// ShardCount is the number of parallel Parquet writers. Defaults to
	// runtime.NumCPU(). Subscribers hash to a shard via (sub_id % N).
	ShardCount int

	// PerShardBuffer is the channel buffer size for each shard. Default
	// 16384 (per event-schema.md "Channel sizing"). Larger buffers
	// absorb burst better at the cost of memory.
	PerShardBuffer int

	// FlushInterval is how often each shard's buffer is written to its
	// Parquet file. Default 30s. Final flush also fires on Stop.
	FlushInterval time.Duration

	// BatchSize is the in-memory accumulation cap before forcing an
	// intermediate flush regardless of FlushInterval. Default 4096.
	BatchSize int

	// RotateBytes is the per-file size cap; when exceeded the writer
	// rotates to a new file. Default 256 MiB per contract.
	RotateBytes int64

	// OutcomeBuffer is the buffered-channel size for SubscriberOutcome
	// records (one per subscriber, written near end-of-run). Default 4096.
	OutcomeBuffer int
}

// Defaults applied by New when a field is the zero value.
const (
	defaultPerShardBuffer = 16384
	defaultFlushInterval  = 30 * time.Second
	defaultBatchSize      = 4096
	defaultOutcomeBuffer  = 4096
)

// Collector is the entry point. Construct via New, push via Submit /
// SubmitOutcome, drain via Stop.
type Collector struct {
	opts Opts

	shardCh   []chan events.Event
	outcomeCh chan events.SubscriberOutcome

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopped  atomic.Bool
	stopCh   chan struct{}

	// startedAt records when New() returned. Used as the t=0 anchor by
	// the aggregator when the input event stream is the post-aggregate
	// path (events themselves carry OffsetMs already).
	startedAt time.Time

	// Shared aggregation state. Each shard appends summary fragments to
	// its own slice (no contention). Aggregate() reads them all under
	// the mutex after Stop() has returned.
	aggMu        sync.Mutex
	aggregations []*shardAggregation
	outcomes     []events.SubscriberOutcome

	// dropped is the count of events submitted after Stop() — these
	// would otherwise panic on a closed channel.
	dropped atomic.Int64

	// Parquet writers (owned by their shard goroutine; here only so
	// Stop can flush + close from the same callsite).
	writers      []*parquetWriter
	outcomeWrite *outcomeWriter
}

// New constructs and starts a Collector. The collector starts ShardCount+1
// background goroutines (one per shard plus one for outcomes) and is ready
// to accept Submit / SubmitOutcome calls when this returns. Caller MUST
// call Stop to flush and release resources.
func New(opts Opts) (*Collector, error) {
	if opts.Dir == "" {
		return nil, errors.New("collector: Dir is required")
	}
	if opts.ShardCount <= 0 {
		opts.ShardCount = runtime.NumCPU()
	}
	if opts.PerShardBuffer <= 0 {
		opts.PerShardBuffer = defaultPerShardBuffer
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = defaultFlushInterval
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}
	if opts.OutcomeBuffer <= 0 {
		opts.OutcomeBuffer = defaultOutcomeBuffer
	}

	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", opts.Dir, err)
	}

	c := &Collector{
		opts:         opts,
		shardCh:      make([]chan events.Event, opts.ShardCount),
		outcomeCh:    make(chan events.SubscriberOutcome, opts.OutcomeBuffer),
		stopCh:       make(chan struct{}),
		startedAt:    time.Now().UTC(),
		aggregations: make([]*shardAggregation, opts.ShardCount),
		writers:      make([]*parquetWriter, opts.ShardCount),
		outcomeWrite: newOutcomeWriter(opts.Dir),
	}

	for i := 0; i < opts.ShardCount; i++ {
		c.shardCh[i] = make(chan events.Event, opts.PerShardBuffer)
		c.writers[i] = newParquetWriter(opts.Dir, i, opts.RotateBytes)
		c.aggregations[i] = newShardAggregation()
		c.wg.Add(1)
		go c.runShard(i)
	}

	c.wg.Add(1)
	go c.runOutcome()

	return c, nil
}

// Submit pushes one event to the appropriate shard. Non-blocking IFF the
// shard's buffer has room. Selects on stopCh to avoid send-on-closed-chan
// after Stop. Events submitted after Stop are counted in Dropped() and
// silently discarded.
//
// Hot path. No allocation, no map writes, no locking.
func (c *Collector) Submit(e events.Event) {
	if c.stopped.Load() {
		c.dropped.Add(1)
		return
	}
	shard := int(e.SubscriberID) % c.opts.ShardCount
	if shard < 0 {
		// SubscriberID is uint32 so this won't happen; defensive.
		shard = -shard
	}
	select {
	case c.shardCh[shard] <- e:
	case <-c.stopCh:
		c.dropped.Add(1)
	}
}

// SubmitOutcome pushes one SubscriberOutcome. Same semantics as Submit:
// non-blocking once Stop has fired, silently discards thereafter.
func (c *Collector) SubmitOutcome(o events.SubscriberOutcome) {
	if c.stopped.Load() {
		c.dropped.Add(1)
		return
	}
	select {
	case c.outcomeCh <- o:
	case <-c.stopCh:
		c.dropped.Add(1)
	}
}

// Dropped reports the count of events/outcomes rejected because Submit
// was called after Stop. Useful as a smoke-test assertion (should be 0
// in well-behaved tests).
func (c *Collector) Dropped() int64 { return c.dropped.Load() }

// Stop transitions the Collector to drain-and-flush mode. After Stop
// returns, no further events will be persisted. ctx provides the upper
// bound on time spent waiting for shard goroutines to drain — when ctx
// fires the Collector forcibly closes Parquet writers regardless of
// in-flight events.
func (c *Collector) Stop(ctx context.Context) error {
	var firstErr error
	c.stopOnce.Do(func() {
		c.stopped.Store(true)
		close(c.stopCh)
		// Closing the channels signals the shard goroutines to drain
		// what is buffered and exit.
		for i := range c.shardCh {
			close(c.shardCh[i])
		}
		close(c.outcomeCh)

		done := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All shards drained cleanly.
		case <-ctx.Done():
			firstErr = fmt.Errorf("collector stop: %w", ctx.Err())
		}

		// Final close on writers. Safe to call even if a shard was
		// stuck — its writer may have partial data, but the file is
		// still well-formed.
		for _, w := range c.writers {
			if err := w.close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := c.outcomeWrite.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return firstErr
}

// runShard is the per-shard background loop. Reads from shardCh, batches
// events into a slice up to BatchSize, flushes the batch to Parquet
// when the batch fills OR the flush ticker fires OR the channel closes.
//
// Also accumulates per-shard aggregation counters as events stream
// through, so Aggregate() at the end is a cheap reduction step rather
// than a full Parquet re-read.
func (c *Collector) runShard(shardID int) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.opts.FlushInterval)
	defer ticker.Stop()

	w := c.writers[shardID]
	agg := c.aggregations[shardID]

	batch := make([]events.Event, 0, c.opts.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.writeBatch(batch); err != nil {
			// We can't propagate from inside a worker; record on the
			// shard aggregation so the final summary can surface it.
			agg.recordWriteError(err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e, ok := <-c.shardCh[shardID]:
			if !ok {
				flush()
				return
			}
			agg.observe(e)
			batch = append(batch, e)
			if len(batch) >= c.opts.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// runOutcome is the parallel loop for SubscriberOutcome. There is only one
// of these (outcomes are written to a single subscribers.parquet file).
func (c *Collector) runOutcome() {
	defer c.wg.Done()
	batch := make([]events.SubscriberOutcome, 0, c.opts.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := c.outcomeWrite.writeBatch(batch); err != nil {
			c.aggMu.Lock()
			c.aggregations[0].recordWriteError(err)
			c.aggMu.Unlock()
		}
		c.aggMu.Lock()
		c.outcomes = append(c.outcomes, batch...)
		c.aggMu.Unlock()
		batch = batch[:0]
	}

	for o := range c.outcomeCh {
		batch = append(batch, o)
		if len(batch) >= c.opts.BatchSize {
			flush()
		}
	}
	flush()
}

// EventsPath returns the relative artifact path used in summary.json. The
// collector intentionally does not pin a single events.parquet name — it
// writes one file per shard. summary.json refers to the directory; the
// reader globs events-shard-*.parquet.
func (c *Collector) EventsPath() string { return "events.parquet" }

// SubscribersPath returns the relative path of the subscribers parquet.
func (c *Collector) SubscribersPath() string {
	return filepath.Base(c.outcomeWrite.path)
}
