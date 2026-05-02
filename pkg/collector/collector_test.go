// Package collector — integration tests covering Submit/Stop/Aggregate
// and Parquet file validity.
//
// Purpose:
//
//	Black-box tests for the sharded collector. Verifies zero loss under
//	concurrent submission, end-of-test aggregation correctness, and that
//	the produced Parquet files are readable with the canonical reader
//	for the schema.
//
// Related files:
//   - pkg/collector/collector.go (subject under test)
//   - pkg/collector/parquet.go (writer being verified by Parquet round-trip)
//   - pkg/collector/aggregate.go (consumer for end-of-test summary)
//   - pkg/events/event.go (input record type)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — test-only.
package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEvent returns a minimal but contract-shaped Event for the given subID.
func makeEvent(subID uint32, kind, evType, state string) events.Event {
	return events.Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  int64(subID),
		OffsetMs:     int64(subID),
		SubscriberID: subID,
		Category:     kind,
		EventType:    evType,
		State:        state,
		Identifier:   events.NoIdentifier,
	}
}

// countParquetRows globs every per-shard events file in dir and tallies
// the row count by streaming with a GenericReader.
func countParquetRows(t *testing.T, dir string) int64 {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "events-shard-*.parquet"))
	require.NoError(t, err)
	var total int64
	for _, m := range matches {
		f, err := os.Open(m)
		require.NoError(t, err, "open parquet %s", m)
		stat, err := f.Stat()
		require.NoError(t, err)
		r := parquet.NewGenericReader[events.Event](f)
		// Stream rows one at a time to confirm schema validity.
		buf := make([]events.Event, 256)
		for {
			n, rerr := r.Read(buf)
			total += int64(n)
			if rerr != nil {
				break
			}
		}
		require.NoError(t, r.Close())
		require.NoError(t, f.Close())
		// Sanity: file is non-empty.
		assert.Greater(t, stat.Size(), int64(0), "parquet file %s is empty", m)
	}
	return total
}

func TestNewRequiresDir(t *testing.T) {
	_, err := New(Opts{})
	require.Error(t, err)
}

func TestSubmitStopAggregateBasic(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 2, FlushInterval: 100 * time.Millisecond, BatchSize: 64})
	require.NoError(t, err)

	// Three subscribers, one each: created → activated → terminal=established.
	for sub := uint32(1); sub <= 3; sub++ {
		c.Submit(events.NewSubscriberCreated(sub, 0, "idle"))
		c.Submit(events.NewSubscriberActivated(sub, int64(10*sub), "auth_sent"))
		c.Submit(events.NewTerminal(sub, int64(100*sub), events.FinalStateEstablished))
	}

	require.NoError(t, c.Stop(context.Background()))

	sum, err := c.Aggregate(AggregateOpts{
		RunID:        "run-test",
		ScenarioType: "cold_start",
		Outcome:      OutcomeSucceeded,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), sum.Subscribers.Total)
	assert.Equal(t, int64(3), sum.Subscribers.Established)
	assert.Equal(t, int64(0), sum.Subscribers.StillInFlightAtEnd)
	// Curve never empty when subs exist.
	assert.NotEmpty(t, sum.Establishment.Curve)
}

func TestZeroLossUnderConcurrency(t *testing.T) {
	const (
		producers       = 4
		eventsPerWorker = 25_000
		want            = producers * eventsPerWorker // 100_000
	)
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 4, PerShardBuffer: 16384, FlushInterval: 50 * time.Millisecond, BatchSize: 1024})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for w := 0; w < producers; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; i < eventsPerWorker; i++ {
				subID := uint32(wid*eventsPerWorker + i + 1)
				c.Submit(makeEvent(subID, events.CategorySubscriberLifecycle, events.EventTypeStateChanged, "auth_sent"))
			}
		}(w)
	}
	wg.Wait()

	require.NoError(t, c.Stop(context.Background()))
	got := countParquetRows(t, dir)
	assert.Equal(t, int64(want), got, "every submitted event must be persisted")
	assert.Equal(t, int64(0), c.Dropped(), "no events must have been dropped")
}

func TestSubmitAfterStopIsCounted(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 2})
	require.NoError(t, err)
	require.NoError(t, c.Stop(context.Background()))

	c.Submit(makeEvent(1, events.CategorySubscriberLifecycle, events.EventTypeStateChanged, "x"))
	c.Submit(makeEvent(2, events.CategorySubscriberLifecycle, events.EventTypeStateChanged, "y"))
	c.SubmitOutcome(events.SubscriberOutcome{SubscriberID: 1})

	assert.Equal(t, int64(3), c.Dropped())
}

func TestAggregateBeforeStopErrors(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 1})
	require.NoError(t, err)
	defer c.Stop(context.Background())
	_, err = c.Aggregate(AggregateOpts{})
	require.Error(t, err)
}

func TestAggregateRollupCounts(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 4, FlushInterval: 50 * time.Millisecond})
	require.NoError(t, err)

	// Subscriber 1: established with 1 auth retransmit.
	c.Submit(events.NewSubscriberCreated(1, 0, "idle"))
	c.Submit(events.NewSubscriberActivated(1, 1, "auth_sent"))
	c.Submit(events.NewRequestSent(1, 2, "auth_sent", 1, 10, "", "", 80, 0))
	c.Submit(events.NewRequestSent(1, 3, "auth_retry", 1, 11, "", "", 80, 1)) // retx
	c.Submit(events.NewReplyReceived(1, 5, "auth_sent", 2, 11, "", "", 40, 1500))
	c.Submit(events.NewTerminal(1, 100, events.FinalStateEstablished))

	// Subscriber 2: auth_failed.
	c.Submit(events.NewSubscriberActivated(2, 1, "auth_sent"))
	c.Submit(events.NewTerminal(2, 80, events.FinalStateAuthFailed))

	// Subscriber 3: never activated, still in-flight at end.
	c.Submit(events.NewSubscriberCreated(3, 0, "idle"))

	// CoA acked + naked + dropped.
	c.Submit(events.NewCoAEvent(1, 200, events.EventTypeCoAReceived, 1, "", "", 60, 0, 0))
	c.Submit(events.NewCoAEvent(1, 201, events.EventTypeCoAAcked, 1, "", "", 60, 250, 0))
	c.Submit(events.NewCoAEvent(2, 202, events.EventTypeCoANaked, 2, "", "", 60, 350, 401))
	c.Submit(events.NewCoAEvent(2, 203, events.EventTypeCoADropped, 3, "", "", 60, 0, 0))

	// Disconnect.
	c.Submit(events.NewDisconnectEvent(1, 300, events.EventTypeDisconnectReceived, 5, "", "", 60, 0, 0))
	c.Submit(events.NewDisconnectEvent(1, 301, events.EventTypeDisconnectAcked, 5, "", "", 60, 700, 0))

	// One outcome per subscriber.
	for sub := uint32(1); sub <= 3; sub++ {
		c.SubmitOutcome(events.SubscriberOutcome{SubscriberID: sub, Username: "u", AuthMethod: events.AuthMethodPAP})
	}

	require.NoError(t, c.Stop(context.Background()))
	sum, err := c.Aggregate(AggregateOpts{RunID: "rollup", Outcome: OutcomeFailed,
		Thresholds: []Threshold{
			{Name: "all_established", Expected: "1==1", Actual: "established_1", Pass: true},
			{Name: "p99_latency_ms", Expected: "<=5000", Actual: int64(2000), Pass: true},
		}})
	require.NoError(t, err)

	assert.Equal(t, int64(3), sum.Subscribers.Total)
	assert.Equal(t, int64(1), sum.Subscribers.Established)
	assert.Equal(t, int64(1), sum.Subscribers.AuthFailed)
	assert.Equal(t, int64(1), sum.Subscribers.StillInFlightAtEnd)

	assert.Equal(t, int64(1), sum.Retransmits.Total)
	assert.Equal(t, int64(1), sum.Retransmits.SubscribersWithRetransmit)

	assert.Equal(t, int64(1), sum.CoA.Received)
	assert.Equal(t, int64(1), sum.CoA.Acked)
	assert.Equal(t, int64(1), sum.CoA.Naked)
	assert.Equal(t, int64(1), sum.CoA.Dropped)
	require.NotNil(t, sum.CoA.ResponseLatencyUs)
	assert.GreaterOrEqual(t, sum.CoA.ResponseLatencyUs.Max, int64(350))

	assert.Equal(t, int64(1), sum.Disconnect.Received)
	assert.Equal(t, int64(1), sum.Disconnect.Acked)
	require.NotNil(t, sum.Disconnect.ResponseLatencyUs)

	assert.Equal(t, ThresholdsOverallPass, sum.Thresholds.Overall)
	assert.Len(t, sum.Thresholds.Evaluated, 2)
}

func TestSummaryRoundTripJSON(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 1})
	require.NoError(t, err)
	c.Submit(events.NewTerminal(1, 100, events.FinalStateEstablished))
	c.Submit(events.NewSubscriberActivated(1, 0, "auth_sent"))
	require.NoError(t, c.Stop(context.Background()))
	sum, err := c.Aggregate(AggregateOpts{RunID: "rt", ScenarioType: "cold_start", Outcome: OutcomeSucceeded})
	require.NoError(t, err)

	require.NoError(t, c.WriteSummary(sum))

	jb, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	require.NoError(t, err)
	var back Summary
	require.NoError(t, json.Unmarshal(jb, &back))
	assert.Equal(t, sum.RunID, back.RunID)
	assert.Equal(t, sum.Subscribers.Established, back.Subscribers.Established)
	assert.Equal(t, sum.Outcome, back.Outcome)

	// Human-readable file present.
	tb, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(tb), "radstorm run summary")
}

func TestParquetRoundTripPreservesFields(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 1, FlushInterval: 50 * time.Millisecond})
	require.NoError(t, err)

	in := events.Event{
		Timestamp:    time.Unix(1715000000, 0).UTC(),
		MonotonicNs:  12345,
		OffsetMs:     67,
		SubscriberID: 7,
		Category:     events.CategoryPacketOutbound,
		EventType:    events.EventTypeRequestSent,
		State:        "auth_sent",
		RadiusCode:   1,
		Identifier:   42,
		LocalAddr:    "10.0.0.1:1812",
		RemoteAddr:   "10.0.0.2:1812",
		PacketBytes:  84,
		LatencyUs:    0,
		RetransmitN:  0,
		ErrorCause:   0,
		ErrorMessage: "",
		Tags:         map[string]string{"k": "v"},
	}
	c.Submit(in)
	require.NoError(t, c.Stop(context.Background()))

	matches, err := filepath.Glob(filepath.Join(dir, "events-shard-*.parquet"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	f, err := os.Open(matches[0])
	require.NoError(t, err)
	defer f.Close()
	r := parquet.NewGenericReader[events.Event](f)
	defer r.Close()

	buf := make([]events.Event, 1)
	n, _ := r.Read(buf)
	require.Equal(t, 1, n)
	got := buf[0]
	assert.Equal(t, in.SubscriberID, got.SubscriberID)
	assert.Equal(t, in.Category, got.Category)
	assert.Equal(t, in.EventType, got.EventType)
	assert.Equal(t, in.State, got.State)
	assert.Equal(t, in.RadiusCode, got.RadiusCode)
	assert.Equal(t, in.Identifier, got.Identifier)
	assert.Equal(t, in.LocalAddr, got.LocalAddr)
	assert.Equal(t, in.RemoteAddr, got.RemoteAddr)
	assert.Equal(t, in.PacketBytes, got.PacketBytes)
	assert.Equal(t, in.MonotonicNs, got.MonotonicNs)
	assert.Equal(t, in.OffsetMs, got.OffsetMs)
	assert.Equal(t, in.Tags, got.Tags)
}

func TestArtifactPathsAreStable(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 1})
	require.NoError(t, err)
	defer c.Stop(context.Background())
	assert.Equal(t, "events.parquet", c.EventsPath())
	assert.Equal(t, "subscribers.parquet", c.SubscribersPath())
}

func TestSummaryTextRendersThresholds(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 1})
	require.NoError(t, err)
	c.Submit(events.NewTerminal(1, 1, events.FinalStateEstablished))
	require.NoError(t, c.Stop(context.Background()))
	sum, err := c.Aggregate(AggregateOpts{
		RunID:   "rt",
		Outcome: OutcomeSucceeded,
		Thresholds: []Threshold{
			{Name: "ok_one", Expected: 1, Actual: 1, Pass: true},
			{Name: "fail_one", Expected: 1, Actual: 0, Pass: false},
		},
	})
	require.NoError(t, err)
	require.NoError(t, c.WriteSummary(sum))
	tb, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	require.NoError(t, err)
	body := string(tb)
	assert.Contains(t, body, "[PASS] ok_one")
	assert.Contains(t, body, "[FAIL] fail_one")
}

func TestParquetRotation(t *testing.T) {
	dir := t.TempDir()
	// Force rotation after a tiny number of bytes — every write triggers
	// a new file (covers rotate()).
	c, err := New(Opts{Dir: dir, ShardCount: 1, RotateBytes: 1, BatchSize: 16})
	require.NoError(t, err)
	for i := uint32(1); i <= 200; i++ {
		c.Submit(makeEvent(i, events.CategorySubscriberLifecycle, events.EventTypeStateChanged, "x"))
	}
	require.NoError(t, c.Stop(context.Background()))
	matches, err := filepath.Glob(filepath.Join(dir, "events-shard-*.parquet"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(matches), 2, "rotation should have produced multiple files")
	assert.Equal(t, int64(200), countParquetRows(t, dir))
}

func TestSubscribersParquetWritten(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{Dir: dir, ShardCount: 1})
	require.NoError(t, err)
	for sub := uint32(1); sub <= 5; sub++ {
		c.SubmitOutcome(events.SubscriberOutcome{
			SubscriberID:           sub,
			Username:               "u",
			AuthMethod:             events.AuthMethodPAP,
			SubType:                events.SubTypePPPoE,
			FinalState:             events.FinalStateEstablished,
			ActivatedAtOffsetMs:    int64(sub),
			EstablishedAtOffsetMs:  int64(sub * 10),
			EstablishmentLatencyMs: int64(sub * 9),
		})
	}
	require.NoError(t, c.Stop(context.Background()))

	path := filepath.Join(dir, "subscribers.parquet")
	stat, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(0))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	r := parquet.NewGenericReader[events.SubscriberOutcome](f)
	defer r.Close()
	buf := make([]events.SubscriberOutcome, 16)
	var total int
	for {
		n, rerr := r.Read(buf)
		total += n
		if rerr != nil {
			break
		}
	}
	assert.Equal(t, 5, total)
}

// TestPerformance1MEvents verifies the briefing's perf target: submit 1M
// events from 8 producers in <5s with no loss. Buffers sized to fit the
// full event volume so drop-on-full Submit doesn't shed under burst load
// faster than the shard goroutines can drain.
//
// The race detector adds 5–10x overhead and would fail the timing gate,
// so the timing assertion is skipped under -race; correctness assertions
// still run.
func TestPerformance1MEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("perf test skipped under -short")
	}
	const (
		producers       = 8
		eventsPerWorker = 125_000
		want            = producers * eventsPerWorker // 1_000_000
	)
	dir := t.TempDir()
	c, err := New(Opts{
		Dir:        dir,
		ShardCount: 8,
		// Per-shard buffer sized to absorb a tight-loop burst (1M events /
		// 8 shards = 125k per shard, doubled for safety). With drop-on-full
		// Submit, undersized buffers would shed events faster than the
		// shard goroutines drain them.
		PerShardBuffer: 256 * 1024,
		FlushInterval:  250 * time.Millisecond,
		BatchSize:      4096,
	})
	require.NoError(t, err)

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < producers; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; i < eventsPerWorker; i++ {
				subID := uint32(wid*eventsPerWorker + i + 1)
				c.Submit(makeEvent(subID, events.CategorySubscriberLifecycle, events.EventTypeStateChanged, "auth_sent"))
			}
		}(w)
	}
	wg.Wait()
	require.NoError(t, c.Stop(context.Background()))
	elapsed := time.Since(start)
	if !raceEnabled {
		assert.Less(t, elapsed, 5*time.Second, "1M events submit+drain took %v", elapsed)
	} else {
		t.Logf("race detector enabled — perf gate relaxed; took %v", elapsed)
	}
	assert.Equal(t, int64(0), c.Dropped(), "no events should be dropped after Stop in this test")
	assert.Equal(t, int64(0), c.EventsDroppedFull(), "no events should be dropped due to back-pressure with sized buffers")

	got := countParquetRows(t, dir)
	assert.Equal(t, int64(want), got)
}

// TestSubmit_DropOnFull_PreservesMeasurementIntegrity verifies the
// measurement-integrity contract: when the shard channel saturates,
// Submit drops events (does NOT block) and the drop is reported in
// the integrity counters and the summary.
//
// This is the dual of TestPerformance1MEvents — same workload but with
// an artificially small buffer so back-pressure is guaranteed.
func TestSubmit_DropOnFull_PreservesMeasurementIntegrity(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Opts{
		Dir:            dir,
		ShardCount:     1,
		PerShardBuffer: 4, // tiny: forces drops on tight-loop submit
		FlushInterval:  500 * time.Millisecond,
		BatchSize:      2,
	})
	require.NoError(t, err)

	const total = 5_000
	for i := 0; i < total; i++ {
		c.Submit(makeEvent(uint32(i+1), events.CategorySubscriberLifecycle, events.EventTypeStateChanged, "auth_sent"))
	}

	require.NoError(t, c.Stop(context.Background()))

	// We expect a substantial fraction to be dropped given the buffer is
	// 4 events deep against 5000 submissions.
	dropped := c.EventsDroppedFull()
	assert.Greater(t, dropped, int64(0), "tight-loop submit with tiny buffer must drop")
	assert.Equal(t, int64(total), c.TotalSubmitted()+dropped,
		"submitted + dropped should account for every Submit call")

	// Integrity report should reflect the drops with low/medium trust.
	sum, err := c.Aggregate(AggregateOpts{RunID: "drop-test"})
	require.NoError(t, err)
	require.NotNil(t, sum)

	mi := sum.MeasurementIntegrity
	assert.Contains(t, []string{"low", "medium"}, mi.Trust,
		"trust must downgrade when drops occur")
	assert.Equal(t, dropped, mi.EventsDroppedBackPressure)
	assert.NotNil(t, mi.FirstDropOffsetMs)
	assert.NotNil(t, mi.LastDropOffsetMs)
}
