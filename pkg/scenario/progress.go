// Package scenario — progress.jsonl writer.
//
// Purpose:
//
//	Background goroutine that emits one JSON line per second describing
//	the live state of a scenario run. The API server (slice 3B) tails this
//	file to drive an SSE stream; the frontend renders the activated /
//	established / in-flight curve from it.
//
//	The writer is a pure consumer of counters held by the Runner —
//	those counters are populated as events are observed; the progress
//	writer just samples them on a tick and serialises one line.
//
// Related files:
//   - pkg/scenario/runner.go    (owns the live counters this samples)
//   - pkg/scenario/lifecycle.go (Phase string written into each line)
//   - apps/api                  (consumer — tails progress.jsonl)
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
//
// Contract: Public — Snapshot's JSON tags and Phase values are part of
// the SSE wire shape consumed by the REST API and the frontend. Fields
// can be added but never renamed/removed without a contract amendment.
package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Snapshot is one progress sample. Marshalled as a single JSON object on
// its own line in progress.jsonl (NDJSON / JSON-lines format).
type Snapshot struct {
	OffsetMs    int64  `json:"offset_ms"`
	Activated   int64  `json:"activated"`
	Established int64  `json:"established"`
	Failed      int64  `json:"failed"`
	InFlight    int64  `json:"in_flight"`
	Retransmits int64  `json:"retransmits"`
	Phase       string `json:"phase"`
}

// liveCounters is the shared state the runner updates and the progress
// writer samples. All fields are atomic so reads from the writer
// goroutine never tear and never block writers.
type liveCounters struct {
	activated   atomic.Int64
	established atomic.Int64
	failed      atomic.Int64
	retransmits atomic.Int64

	// phase is updated by the runner via setPhase; the writer reads via
	// loadPhase. The wrapping pointer lets us swap the value atomically.
	phasePtr atomic.Pointer[Phase]
}

// newLiveCounters returns a counters instance pre-set to PhaseWarmup.
func newLiveCounters() *liveCounters {
	c := &liveCounters{}
	p := PhaseWarmup
	c.phasePtr.Store(&p)
	return c
}

// setPhase records a phase transition observable by the writer.
func (c *liveCounters) setPhase(p Phase) {
	pCopy := p
	c.phasePtr.Store(&pCopy)
}

// loadPhase returns the current phase (PhaseWarmup if none set).
func (c *liveCounters) loadPhase() Phase {
	if p := c.phasePtr.Load(); p != nil {
		return *p
	}
	return PhaseWarmup
}

// inFlight returns activated - (established + failed). Clamped to >= 0
// because a race between establishing and a parallel failure increment
// could briefly produce a negative; the over-counted side will catch up
// on the next tick.
func (c *liveCounters) inFlight() int64 {
	v := c.activated.Load() - c.established.Load() - c.failed.Load()
	if v < 0 {
		return 0
	}
	return v
}

// progressWriter drives the per-second snapshot loop and owns the
// underlying file handle. Stop blocks until the background goroutine
// exits.
type progressWriter struct {
	path    string
	t0      time.Time
	counts  *liveCounters
	tickDur time.Duration

	mu     sync.Mutex
	file   io.WriteCloser
	stopCh chan struct{}
	doneCh chan struct{}
}

// progressOpts configures the writer.
type progressOpts struct {
	OutputDir string
	T0        time.Time
	Counters  *liveCounters
	Tick      time.Duration // default 1s
}

// newProgressWriter constructs a writer and opens the destination file.
// Returns the writer in a stopped state — call Start to launch the
// background goroutine.
func newProgressWriter(opts progressOpts) (*progressWriter, error) {
	if opts.OutputDir == "" {
		return nil, errors.New("scenario: progress writer requires OutputDir")
	}
	if opts.Counters == nil {
		return nil, errors.New("scenario: progress writer requires Counters")
	}
	if opts.Tick <= 0 {
		opts.Tick = time.Second
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("scenario: progress mkdir %s: %w", opts.OutputDir, err)
	}
	path := filepath.Join(opts.OutputDir, "progress.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("scenario: progress open %s: %w", path, err)
	}

	pw := &progressWriter{
		path:    path,
		t0:      opts.T0,
		counts:  opts.Counters,
		tickDur: opts.Tick,
		file:    f,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	if pw.t0.IsZero() {
		pw.t0 = time.Now()
	}
	return pw, nil
}

// Path returns the absolute path of the progress.jsonl file.
func (w *progressWriter) Path() string { return w.path }

// Start launches the background sampler. ctx cancellation also stops
// the writer (it is the parent's signal to drain).
func (w *progressWriter) Start(ctx context.Context) {
	go w.run(ctx)
}

// Stop signals the writer to exit and blocks until the sampler
// goroutine has flushed the final snapshot and closed the file.
func (w *progressWriter) Stop() {
	select {
	case <-w.stopCh:
		// Already stopped.
	default:
		close(w.stopCh)
	}
	<-w.doneCh
}

// run is the sampler loop. Emits one snapshot immediately so consumers
// see at least one line even if the run terminates faster than the
// tick interval, then ticks until ctx fires or Stop is called. Always
// flushes a final snapshot before returning so the last reading is
// durable.
func (w *progressWriter) run(ctx context.Context) {
	defer close(w.doneCh)
	defer func() {
		w.mu.Lock()
		_ = w.file.Close()
		w.mu.Unlock()
	}()

	w.writeOne()

	t := time.NewTicker(w.tickDur)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			w.writeOne()
			return
		case <-w.stopCh:
			w.writeOne()
			return
		case <-t.C:
			w.writeOne()
		}
	}
}

// writeOne samples the counters and appends one JSON line. Errors are
// swallowed — progress is best-effort observability and must not crash
// the run.
func (w *progressWriter) writeOne() {
	snap := Snapshot{
		OffsetMs:    time.Since(w.t0).Milliseconds(),
		Activated:   w.counts.activated.Load(),
		Established: w.counts.established.Load(),
		Failed:      w.counts.failed.Load(),
		InFlight:    w.counts.inFlight(),
		Retransmits: w.counts.retransmits.Load(),
		Phase:       string(w.counts.loadPhase()),
	}
	b, err := json.Marshal(&snap)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.file.Write(b)
	_, _ = w.file.Write([]byte{'\n'})
}
