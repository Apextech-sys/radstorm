// Package scenario — Runner that wires every component into a complete run.
//
// Purpose:
//
//	The Runner is the integration seam between every previously-merged
//	package. Given a Config + Credentials + OutputDir it:
//
//	  1. Constructs a Collector (writes to OutputDir)
//	  2. Constructs an io.Engine (binds source IPs/ports, registers the
//	     server-listener as the in-process ServerHandler so server-initiated
//	     packets received on a SUBSCRIBER socket are routed correctly)
//	  3. Constructs a subscriber.Pool (one Subscriber per credential)
//	  4. Constructs a server.Listener (CoA / Disconnect endpoint, Pool as
//	     SubscriberLookup)
//	  5. Builds the activation Schedule
//	  6. Runs the Lifecycle: Warmup → Ramp (drive activations off the
//	     schedule) → Drain (cancel new activations, wait in-flight) →
//	     Finalize (Stop Collector, write summary, Stop listener+engine)
//
// Related files:
//   - pkg/scenario/schedule.go   (activation schedule generators)
//   - pkg/scenario/lifecycle.go  (Phase enum + transition validation)
//   - pkg/scenario/progress.go   (progress.jsonl writer)
//   - pkg/collector              (event sink + final summary)
//   - pkg/io                     (UDP I/O engine)
//   - pkg/subscriber             (per-subscriber FSM + pool)
//   - pkg/server                 (CoA/Disconnect listener)
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
//
// Contract: Public — Opts and Runner are the public surface consumed by
// the CLI (apps/cli/cmd/radstorm) and the API (apps/api).
package scenario

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/collector"
	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/io"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
	"github.com/Apextech-sys/reflex-radstorm/pkg/server"
	"github.com/Apextech-sys/reflex-radstorm/pkg/subscriber"
)

// Opts is the public Runner configuration.
type Opts struct {
	// Config is the parsed scenario config. Required.
	Config *config.Config

	// Creds is the credential pool (one row per subscriber). Required.
	Creds []config.Credential

	// OutputDir is where the Collector writes parquet + summary files
	// AND where progress.jsonl is emitted. Required.
	OutputDir string

	// Logger is the operational slog logger. Optional; defaults to slog.Default.
	Logger *slog.Logger

	// Seed is the deterministic RNG seed used for cold_start scheduling.
	// Zero = time-based seed.
	Seed int64

	// DrainTimeout bounds the Drain phase. Defaults to 30s.
	DrainTimeout time.Duration

	// WarmupDelay is how long Warmup waits after binding sockets and
	// before starting Ramp. Defaults to 1s.
	WarmupDelay time.Duration

	// SkipServerListener disables the CoA/Disconnect listener. Useful in
	// unit tests where binding 0.0.0.0:3799 isn't available.
	SkipServerListener bool

	// CollectorOverride lets tests inject a pre-built Collector (for
	// in-memory recording). When nil, a real Collector is constructed
	// against OutputDir.
	CollectorOverride CollectorAPI

	// EngineOverride lets tests inject a fake io.Sender + LocalAddrs.
	// When nil a real io.Engine is constructed.
	EngineOverride EngineAPI

	// ConfigSource is the serialised TOML for the loaded config. Used to
	// compute the config_hash that lands in summary.json. Optional; when
	// nil, the hash will be empty.
	ConfigSource []byte

	// RunID is the run identifier embedded in summary.json. Optional;
	// when empty a timestamp-based ID is generated.
	RunID string
}

// CollectorAPI is the surface the Runner uses against the collector. The
// real *collector.Collector satisfies it; tests provide an in-memory fake.
type CollectorAPI interface {
	Submit(events.Event)
	SubmitOutcome(events.SubscriberOutcome)
	Stop(ctx context.Context) error
	Aggregate(opts collector.AggregateOpts) (*collector.Summary, error)
	WriteSummary(s *collector.Summary) error
}

// EngineAPI is the surface the Runner uses against the I/O engine. The
// real *io.Engine satisfies it; tests substitute a fake that returns
// pre-canned SendResults so a runner can be exercised without UDP.
type EngineAPI interface {
	io.Sender
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	LocalAddrs() []*net.UDPAddr
}

// Runner is the central orchestrator. Construct via New, drive via Run,
// inspect post-run via Counters / Phase.
type Runner struct {
	opts Opts
	log  *slog.Logger

	cfg   *config.Config
	creds []config.Credential

	t0       time.Time
	counters *liveCounters

	collector CollectorAPI
	engine    EngineAPI
	pool      *subscriber.Pool
	listener  *server.Listener
	progress  *progressWriter
	schedule  *Schedule

	authDst net.Addr
	acctDst net.Addr

	// owned tracks whether we own (and therefore must Stop) each
	// component. When the caller injects an override we don't.
	ownsCollector bool
	ownsEngine    bool

	mu           sync.Mutex
	currentPhase Phase

	// activeWG tracks in-flight subscriber goroutines launched in ramp.
	// drain waits on it. Pointer-typed so a coa_storm/no-launch run can
	// leave it nil and the drain branch handles it cleanly.
	activeWG *sync.WaitGroup

	// runErr captures the first hard error observed during the run; the
	// scenario still tries to drain + finalize so summary.json is written.
	runErr error
}

// Defaults applied when the corresponding Opts field is the zero value.
const (
	defaultDrainTimeout = 30 * time.Second
	defaultWarmupDelay  = 1 * time.Second
)

// Errors returned by New / Run.
var (
	ErrNilConfig   = errors.New("scenario: Opts.Config is nil")
	ErrNoCreds     = errors.New("scenario: Opts.Creds is empty")
	ErrNoOutputDir = errors.New("scenario: Opts.OutputDir is empty")
)

// New constructs a Runner. Heavy work (binding sockets, creating
// goroutines) does NOT happen here — Run does that. New only validates
// Opts and pre-computes the activation schedule so config errors fail
// fast.
func New(opts Opts) (*Runner, error) {
	if opts.Config == nil {
		return nil, ErrNilConfig
	}
	if opts.OutputDir == "" {
		return nil, ErrNoOutputDir
	}
	if len(opts.Creds) == 0 && opts.Config.Subscribers.Count > 0 && opts.Config.Scenario.Type != "coa_storm" {
		return nil, ErrNoCreds
	}
	if opts.DrainTimeout <= 0 {
		opts.DrainTimeout = defaultDrainTimeout
	}
	if opts.WarmupDelay < 0 {
		opts.WarmupDelay = defaultWarmupDelay
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	sched, err := BuildSchedule(opts.Config, opts.Seed)
	if err != nil {
		return nil, fmt.Errorf("scenario: build schedule: %w", err)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("scenario: mkdir output %s: %w", opts.OutputDir, err)
	}

	r := &Runner{
		opts:         opts,
		log:          opts.Logger,
		cfg:          opts.Config,
		creds:        opts.Creds,
		schedule:     sched,
		counters:     newLiveCounters(),
		currentPhase: PhaseWarmup,
	}
	if opts.RunID == "" {
		r.opts.RunID = fmt.Sprintf("run-%d", time.Now().UTC().Unix())
	}
	return r, nil
}

// Phase returns the current lifecycle phase. Safe for concurrent reads.
func (r *Runner) Phase() Phase {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentPhase
}

// Schedule returns the pre-computed activation schedule.
func (r *Runner) Schedule() *Schedule { return r.schedule }

// LiveCounters exposes the live snapshot counters (read-only). Useful for
// tests that want to assert on activated/established without parsing the
// progress file.
func (r *Runner) LiveCounters() *liveCounters { return r.counters }

// Run executes the full scenario. Blocks until the run completes or ctx
// is cancelled (which triggers an orderly Drain + Finalize).
func (r *Runner) Run(ctx context.Context) error {
	r.t0 = time.Now()

	if err := r.constructComponents(ctx); err != nil {
		return err
	}
	defer r.shutdown(ctx)

	if err := r.warmup(ctx); err != nil {
		r.runErr = errors.Join(r.runErr, err)
	}

	if r.runErr == nil {
		r.ramp(ctx)
	}

	r.drain(ctx)

	if err := r.finalize(ctx); err != nil {
		r.runErr = errors.Join(r.runErr, err)
	}

	r.transition(PhaseDone)
	return r.runErr
}

// constructComponents builds the collector, engine, listener, pool, and
// progress writer. Each subsystem owns its own lifecycle — failures here
// abort the run entirely (nothing to drain yet).
func (r *Runner) constructComponents(ctx context.Context) error {
	// Resolve target addresses up-front so we surface DNS errors before
	// we bind sockets.
	authAddr, err := net.ResolveUDPAddr("udp", r.cfg.Target.AuthAddress)
	if err != nil {
		return fmt.Errorf("scenario: resolve target.auth_address: %w", err)
	}
	r.authDst = authAddr
	if r.cfg.Subscribers.IncludeAcctStart {
		acctAddr, err := net.ResolveUDPAddr("udp", r.cfg.Target.AcctAddress)
		if err != nil {
			return fmt.Errorf("scenario: resolve target.acct_address: %w", err)
		}
		r.acctDst = acctAddr
	}

	// Collector --------------------------------------------------------
	if r.opts.CollectorOverride != nil {
		r.collector = r.opts.CollectorOverride
	} else {
		c, err := collector.New(collector.Opts{
			Dir:           r.opts.OutputDir,
			FlushInterval: time.Duration(r.cfg.Output.FlushIntervalSec) * time.Second,
		})
		if err != nil {
			return fmt.Errorf("scenario: collector: %w", err)
		}
		r.collector = c
		r.ownsCollector = true
	}

	// Subscriber pool -------------------------------------------------
	r.pool = subscriber.NewPool(r.creds, r.cfg)

	// I/O engine -------------------------------------------------------
	if r.opts.EngineOverride != nil {
		r.engine = r.opts.EngineOverride
	} else {
		ips, err := parseSourceIPs(r.cfg.Source.IPs)
		if err != nil {
			return fmt.Errorf("scenario: parse source IPs: %w", err)
		}
		eng, err := io.NewEngine(io.Opts{
			SourceIPs:   ips,
			PortRangeLo: r.cfg.Source.PortRange[0],
			PortRangeHi: r.cfg.Source.PortRange[1],
			Secret:      []byte(r.cfg.Target.SharedSecret),
			Collector:   collectorBridgeFor(r.collector),
			// The engine's Handler routes server-initiated packets
			// received on subscriber sockets to the same in-process
			// flow the listener uses. We register a thin adapter that
			// dispatches via the Pool.
			Handler: io.ServerHandlerFunc(func(p *radius.Packet, _ []byte, _ net.Addr, _ net.Addr) {
				r.dispatchServerInitiated(p)
			}),
		})
		if err != nil {
			return fmt.Errorf("scenario: io engine: %w", err)
		}
		r.engine = eng
		r.ownsEngine = true
	}

	// Server listener (skip on opt-out for tests / when bind is impossible).
	if !r.opts.SkipServerListener {
		bind := r.cfg.CoaListener.BindAddress
		secret := r.cfg.CoaListener.SharedSecret
		if bind != "" && secret != "" {
			lst, err := server.New(server.Opts{
				BindAddress:  bind,
				SharedSecret: []byte(secret),
				Lookup:       poolLookupAdapter{p: r.pool},
				Collector:    listenerCollectorBridge{c: r.collector},
				TestT0:       r.t0,
				Logger:       r.log,
			})
			if err != nil {
				// CoA listener is not strictly required for a run; log
				// and continue. Operator can opt-out via SkipServerListener
				// for environments without 0.0.0.0:3799 available.
				r.log.Warn("scenario: server listener bind failed; continuing without CoA endpoint", "err", err)
			} else {
				r.listener = lst
			}
		}
	}

	// Progress writer --------------------------------------------------
	pw, err := newProgressWriter(progressOpts{
		OutputDir: r.opts.OutputDir,
		T0:        r.t0,
		Counters:  r.counters,
	})
	if err != nil {
		return fmt.Errorf("scenario: progress writer: %w", err)
	}
	r.progress = pw

	return nil
}

// warmup transitions to PhaseWarmup, starts the engine + listener, then
// sleeps WarmupDelay to give listeners time to fully bind. Emits a
// lifecycle_warmup event so consumers see the boundary.
func (r *Runner) warmup(ctx context.Context) error {
	r.transition(PhaseWarmup)

	r.collector.Submit(lifecycleEvent(r.t0, "lifecycle_warmup"))

	if err := r.engine.Start(ctx); err != nil {
		return fmt.Errorf("scenario: engine start: %w", err)
	}
	if r.listener != nil {
		if err := r.listener.Start(ctx); err != nil {
			r.log.Warn("scenario: listener start failed", "err", err)
		}
	}

	r.progress.Start(ctx)

	if r.opts.WarmupDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.opts.WarmupDelay):
		}
	}
	return nil
}

// ramp transitions to PhaseRamp and drives the activation schedule. Each
// subscriber is launched on its own goroutine after the schedule's offset
// has elapsed. Returns when every scheduled subscriber has been launched
// (NOT when each Run completes — drain handles that).
func (r *Runner) ramp(ctx context.Context) {
	r.transition(PhaseRamp)
	r.collector.Submit(lifecycleEvent(r.t0, "lifecycle_ramp"))

	if r.schedule.Total() == 0 {
		// coa_storm or N=0 — nothing to launch; ramp is a no-op and we
		// fall through to drain (which holds for HardTimeoutSec).
		return
	}

	var wg sync.WaitGroup
	r.activeWG = &wg

	bridge := r.collectorAsSubscriberSink()

	rampCtx, cancelRamp := context.WithCancel(ctx)
	defer cancelRamp()

	timer := time.NewTimer(0)
	defer timer.Stop()

	startCount := r.schedule.Total()
	for i := 0; i < startCount; i++ {
		idx := i
		offset := r.schedule.Offsets[idx]

		// Sleep until the activation offset relative to T0.
		due := r.t0.Add(offset)
		now := time.Now()
		var d time.Duration
		if due.After(now) {
			d = due.Sub(now)
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(d)

		select {
		case <-rampCtx.Done():
			// ctx fired — break out and let drain take over. Subscribers
			// already launched continue in flight; new ones are skipped.
			return
		case <-timer.C:
		}

		sub := r.pool.Get(uint32(idx))
		if sub == nil {
			// Should not happen because the pool is sized to schedule.Total
			// in the typical path; defensive.
			continue
		}

		wg.Add(1)
		r.counters.activated.Add(1)
		go func() {
			defer wg.Done()
			deps := subscriber.Deps{
				Sender:    r.engine,
				Collector: bridge,
				Config:    r.cfg,
				AuthDst:   r.authDst,
				AcctDst:   r.acctDst,
				T0:        r.t0,
			}
			state, err := sub.Run(ctx, deps)
			if err != nil {
				r.log.Warn("scenario: subscriber run error", "id", sub.ID(), "err", err)
			}
			switch state {
			case subscriber.StateEstablished:
				r.counters.established.Add(1)
			case subscriber.StateAuthFailed, subscriber.StateAcctFailed:
				r.counters.failed.Add(1)
			}
		}()
	}
}

// drain transitions to PhaseDrain and waits for in-flight subscribers
// (or DrainTimeout) to terminate. The waiting respects ctx so a second
// SIGINT can hard-cut.
func (r *Runner) drain(ctx context.Context) {
	r.transition(PhaseDrain)
	r.collector.Submit(lifecycleEvent(r.t0, "lifecycle_drain"))

	if r.activeWG == nil {
		// No subscribers were launched (coa_storm or n=0). Hold for
		// HardTimeoutSec or until ctx fires so the listener has time
		// to capture inbound CoA traffic.
		hold := time.Duration(r.cfg.Scenario.HardTimeoutSec) * time.Second
		if hold <= 0 {
			hold = r.opts.DrainTimeout
		}
		select {
		case <-ctx.Done():
		case <-time.After(hold):
		}
		return
	}

	done := make(chan struct{})
	go func() {
		r.activeWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(r.opts.DrainTimeout):
		r.log.Warn("scenario: drain timeout exceeded; in-flight subscribers will be abandoned",
			"timeout", r.opts.DrainTimeout)
	case <-ctx.Done():
	}
}

// finalize transitions to PhaseFinalize: stop progress writer, stop
// collector (which flushes parquet), aggregate, write summary, then stop
// the listener and engine.
func (r *Runner) finalize(ctx context.Context) error {
	r.transition(PhaseFinalize)
	r.collector.Submit(lifecycleEvent(r.t0, "lifecycle_finalize"))

	// Progress writer first so the last snapshot reflects the final
	// counter state (which the collector can still update via aggregate).
	if r.progress != nil {
		r.progress.Stop()
	}

	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := r.collector.Stop(stopCtx); err != nil {
		r.log.Warn("scenario: collector stop", "err", err)
	}

	finishedAt := time.Now().UTC()
	sum, err := r.collector.Aggregate(collector.AggregateOpts{
		RunID:        r.opts.RunID,
		ConfigHash:   configHash(r.opts.ConfigSource),
		ScenarioType: r.cfg.Scenario.Type,
		StartedAt:    r.t0.UTC(),
		FinishedAt:   finishedAt,
		Outcome:      collector.OutcomeSucceeded,
	})
	if err != nil {
		return fmt.Errorf("scenario: aggregate: %w", err)
	}
	if err := r.collector.WriteSummary(sum); err != nil {
		return fmt.Errorf("scenario: write summary: %w", err)
	}
	return nil
}

// shutdown is the deferred safety net: stops every component we own. The
// happy path stops them in finalize; this catches partial-construction
// errors and panics.
func (r *Runner) shutdown(ctx context.Context) {
	if r.listener != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = r.listener.Stop(stopCtx)
		cancel()
	}
	if r.engine != nil && r.ownsEngine {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = r.engine.Stop(stopCtx)
		cancel()
	}
}

// transition records the next phase and pushes it to the live counters
// (which the progress writer reads). Validates the canonical order so
// programming errors panic in tests.
func (r *Runner) transition(next Phase) {
	r.mu.Lock()
	prev := r.currentPhase
	if prev == next {
		r.mu.Unlock()
		return
	}
	if err := validateTransition(prev, next); err != nil {
		// Programming error — surface loudly so tests catch it.
		r.mu.Unlock()
		panic(err)
	}
	r.currentPhase = next
	r.mu.Unlock()

	r.counters.setPhase(next)
}

// dispatchServerInitiated is the bridge between the I/O receiver (which
// observes server-initiated packets on a subscriber's socket) and the
// pool (which knows how to find the targeted subscriber). Mirrors the
// listener's flow without sending a reply (the I/O layer does not have
// a reply socket of its own; the test rig generates CoA on the listener
// port).
func (r *Runner) dispatchServerInitiated(p *radius.Packet) {
	if p == nil || r.pool == nil {
		return
	}
	target, ok := r.pool.Lookup(p)
	if !ok {
		return
	}
	switch p.Code {
	case radius.CodeCoARequest:
		target.OnCoA(p)
	case radius.CodeDisconnectRequest:
		target.OnDisconnect(p)
	}
}

// collectorAsSubscriberSink adapts the runner's CollectorAPI into the
// subscriber.Collector interface (subset of the same methods).
func (r *Runner) collectorAsSubscriberSink() subscriber.Collector {
	return subscriberCollectorBridge{c: r.collector}
}

// parseSourceIPs converts the config's []string to []net.IP. Empty or
// invalid entries are rejected.
func parseSourceIPs(in []string) ([]net.IP, error) {
	if len(in) == 0 {
		return nil, errors.New("source.ips is empty")
	}
	out := make([]net.IP, 0, len(in))
	for _, s := range in {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP %q", s)
		}
		out = append(out, ip)
	}
	return out, nil
}

// configHash returns sha256:<hex> for the given source bytes. Empty when
// no source was supplied.
func configHash(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	sum := sha256.Sum256(src)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// lifecycleEvent constructs a tagged subscriber-lifecycle Event with no
// associated subscriber. Used to mark phase transitions on the timeline.
func lifecycleEvent(t0 time.Time, kind string) events.Event {
	return events.Event{
		Timestamp:   time.Now().UTC(),
		MonotonicNs: monotonicNs(),
		OffsetMs:    time.Since(t0).Milliseconds(),
		Category:    events.CategorySubscriberLifecycle,
		EventType:   kind,
		Identifier:  events.NoIdentifier,
	}
}

// monotonicNs returns a monotonic nanosecond reading suitable for
// MonotonicNs on Event. We use time.Now().UnixNano() — Go's time package
// monotonic clock is implicitly carried; the int64 value here is wall-
// clock, which is acceptable for cross-event ordering inside a single run.
func monotonicNs() int64 {
	return time.Now().UnixNano()
}

// poolLookupAdapter adapts *subscriber.Pool to server.SubscriberLookup.
// pkg/subscriber.Pool.Lookup returns subscriber.SubscriberTarget (the
// subscriber-package interface); the listener wants
// server.SubscriberTarget. Both interfaces have identical methods so
// the adapter just re-types the return value.
type poolLookupAdapter struct{ p *subscriber.Pool }

func (a poolLookupAdapter) Lookup(req *radius.Packet) (server.SubscriberTarget, bool) {
	t, ok := a.p.Lookup(req)
	if !ok {
		return nil, false
	}
	return t, true
}

// listenerCollectorBridge adapts CollectorAPI down to server.Collector
// (Submit-only).
type listenerCollectorBridge struct{ c CollectorAPI }

func (b listenerCollectorBridge) Submit(e events.Event) { b.c.Submit(e) }

// subscriberCollectorBridge adapts CollectorAPI down to subscriber.Collector.
type subscriberCollectorBridge struct{ c CollectorAPI }

func (b subscriberCollectorBridge) Submit(e events.Event)                    { b.c.Submit(e) }
func (b subscriberCollectorBridge) SubmitOutcome(o events.SubscriberOutcome) { b.c.SubmitOutcome(o) }

// collectorBridgeFor adapts CollectorAPI to io.Collector (Submit-only).
func collectorBridgeFor(c CollectorAPI) io.Collector {
	return ioCollectorBridge{c: c}
}

type ioCollectorBridge struct{ c CollectorAPI }

func (b ioCollectorBridge) Submit(e events.Event) { b.c.Submit(e) }

// SummaryPath returns the absolute path of the summary.json file the
// runner will write. Useful for the CLI to print on success.
func (r *Runner) SummaryPath() string {
	return filepath.Join(r.opts.OutputDir, "summary.json")
}
