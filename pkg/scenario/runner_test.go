// Package scenario — Runner integration tests with fake engine + collector.
//
// Purpose:
//
//	Verifies the Runner wires together schedule + lifecycle + subscriber
//	pool + collector correctly and emits a usable Summary at the end.
//	Uses in-memory fakes (no real UDP, no Parquet) so tests run in <1s.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package scenario

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/pkg/collector"
	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/events"
	"github.com/Apextech-sys/reflex-radstorm/pkg/io"
	"github.com/Apextech-sys/reflex-radstorm/pkg/radius"
)

// fakeEngine is an EngineAPI that always returns Access-Accept (and
// Accounting-Response when the FSM gets that far).
type fakeEngine struct {
	mu        sync.Mutex
	sendCalls int
}

func (f *fakeEngine) Start(ctx context.Context) error { return nil }
func (f *fakeEngine) Stop(ctx context.Context) error  { return nil }
func (f *fakeEngine) LocalAddrs() []*net.UDPAddr      { return nil }

func (f *fakeEngine) Send(ctx context.Context, dst net.Addr, build func(id uint8) (*radius.Packet, error), policy io.RetransmitPolicy, subID uint32) (*io.SendResult, error) {
	f.mu.Lock()
	f.sendCalls++
	n := f.sendCalls
	f.mu.Unlock()

	pkt, err := build(uint8(n % 256))
	if err != nil {
		return nil, err
	}

	// Decide reply code based on the packet code we were given.
	var replyCode radius.Code
	switch pkt.Code {
	case radius.CodeAccessRequest:
		replyCode = radius.CodeAccessAccept
	case radius.CodeAccountingRequest:
		replyCode = radius.CodeAccountingResponse
	default:
		replyCode = radius.CodeAccessAccept
	}

	return &io.SendResult{
		Reply: &radius.Packet{
			Code:       replyCode,
			Identifier: pkt.Identifier,
		},
		LatencyUs:  150,
		LocalAddr:  fakeUDPAddr{},
		Identifier: pkt.Identifier,
	}, nil
}

type fakeUDPAddr struct{}

func (fakeUDPAddr) Network() string { return "udp" }
func (fakeUDPAddr) String() string  { return "127.0.0.1:0" }

// recordingCollector implements CollectorAPI by recording every event +
// outcome and producing a synthetic Summary on Aggregate.
type recordingCollector struct {
	mu       sync.Mutex
	events   []events.Event
	outcomes []events.SubscriberOutcome
	stopped  bool
	written  *collector.Summary
}

func (r *recordingCollector) Submit(e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.events = append(r.events, e)
}

func (r *recordingCollector) SubmitOutcome(o events.SubscriberOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.outcomes = append(r.outcomes, o)
}

func (r *recordingCollector) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	return nil
}

func (r *recordingCollector) Aggregate(opts collector.AggregateOpts) (*collector.Summary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	established := int64(0)
	authFailed := int64(0)
	acctFailed := int64(0)
	terminated := int64(0)
	for _, o := range r.outcomes {
		switch o.FinalState {
		case events.FinalStateEstablished:
			established++
		case events.FinalStateAuthFailed:
			authFailed++
		case events.FinalStateAcctFailed:
			acctFailed++
		case events.FinalStateTerminated:
			terminated++
		}
	}
	sum := &collector.Summary{
		RunID:        opts.RunID,
		ConfigHash:   opts.ConfigHash,
		StartedAt:    opts.StartedAt,
		FinishedAt:   opts.FinishedAt,
		DurationMs:   opts.FinishedAt.Sub(opts.StartedAt).Milliseconds(),
		ScenarioType: opts.ScenarioType,
		Outcome:      opts.Outcome,
		Subscribers: collector.SubscribersSection{
			Total:       int64(len(r.outcomes)),
			Established: established,
			AuthFailed:  authFailed,
			AcctFailed:  acctFailed,
			Terminated:  terminated,
		},
		Establishment: collector.EstablishmentSection{Curve: []collector.CurvePoint{}},
		Thresholds:    collector.ThresholdsSection{Evaluated: []collector.Threshold{}, Overall: collector.ThresholdsOverallNone},
	}
	return sum, nil
}

func (r *recordingCollector) WriteSummary(s *collector.Summary) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.written = s
	return nil
}

func (r *recordingCollector) outcomeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.outcomes)
}

func smokeConfig(n int) *config.Config {
	return &config.Config{
		Target: config.Target{
			AuthAddress:  "127.0.0.1:11812",
			AcctAddress:  "127.0.0.1:11813",
			SharedSecret: "testing123",
		},
		CoaListener: config.CoaListener{
			BindAddress:  "127.0.0.1:0",
			SharedSecret: "testing123",
		},
		Subscribers: config.Subscribers{
			Count:            n,
			AuthMethodPapPct: 100,
			TypePppoePct:     100,
			IncludeAcctStart: false,
		},
		Source: config.Source{
			IPs:       []string{"127.0.0.1"},
			PortRange: [2]int{0, 0},
		},
		NAS: config.NAS{
			IPAddress:  "127.0.0.1",
			Identifier: "radstorm-test",
		},
		Retransmit: config.Retransmit{
			InitialTimeoutMs: 1000,
			MaxRetries:       1,
			Backoff:          "constant",
			BackoffBaseMs:    100,
		},
		Scenario: config.Scenario{
			Type:           "uniform",
			HardTimeoutSec: 30,
			Uniform:        &config.UniformCfg{DurationSec: 0.1},
		},
		Output: config.Output{
			Directory:        "results",
			FlushIntervalSec: 1,
		},
	}
}

func smokeCreds(n int) []config.Credential {
	out := make([]config.Credential, n)
	for i := 0; i < n; i++ {
		out[i] = config.Credential{
			Username:   "user" + itoa(i),
			Password:   "pw" + itoa(i),
			AuthMethod: "pap",
			SubType:    "pppoe",
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [10]byte
	idx := len(buf)
	for i > 0 {
		idx--
		buf[idx] = '0' + byte(i%10)
		i /= 10
	}
	return string(buf[idx:])
}

func TestRunner_TenSubscribers_AllEstablish(t *testing.T) {
	const n = 10

	cfg := smokeConfig(n)
	creds := smokeCreds(n)
	rc := &recordingCollector{}
	eng := &fakeEngine{}

	tmp := t.TempDir()

	r, err := New(Opts{
		Config:             cfg,
		Creds:              creds,
		OutputDir:          tmp,
		Seed:               1,
		DrainTimeout:       2 * time.Second,
		WarmupDelay:        10 * time.Millisecond,
		SkipServerListener: true,
		CollectorOverride:  rc,
		EngineOverride:     eng,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := rc.outcomeCount(); got != n {
		t.Fatalf("outcomes = %d, want %d", got, n)
	}
	if rc.written == nil {
		t.Fatalf("WriteSummary not called")
	}
	if rc.written.Subscribers.Established != n {
		t.Errorf("established = %d, want %d", rc.written.Subscribers.Established, n)
	}
	if r.Phase() != PhaseDone {
		t.Errorf("final phase = %s, want %s", r.Phase(), PhaseDone)
	}
	if eng.sendCalls < n {
		t.Errorf("sendCalls = %d, want >= %d", eng.sendCalls, n)
	}
}

func TestRunner_NilConfig(t *testing.T) {
	if _, err := New(Opts{OutputDir: t.TempDir()}); err == nil {
		t.Fatalf("expected ErrNilConfig")
	}
}

func TestRunner_NoOutputDir(t *testing.T) {
	if _, err := New(Opts{Config: smokeConfig(1), Creds: smokeCreds(1)}); err == nil {
		t.Fatalf("expected ErrNoOutputDir")
	}
}

func TestRunner_NoCreds(t *testing.T) {
	cfg := smokeConfig(5)
	if _, err := New(Opts{Config: cfg, OutputDir: t.TempDir()}); err == nil {
		t.Fatalf("expected ErrNoCreds")
	}
}

func TestRunner_CtxCancel_TriggersDrain(t *testing.T) {
	const n = 5
	cfg := smokeConfig(n)
	cfg.Scenario.Uniform.DurationSec = 5 // slow ramp
	creds := smokeCreds(n)

	rc := &recordingCollector{}
	eng := &fakeEngine{}

	r, err := New(Opts{
		Config:             cfg,
		Creds:              creds,
		OutputDir:          t.TempDir(),
		DrainTimeout:       1 * time.Second,
		WarmupDelay:        10 * time.Millisecond,
		SkipServerListener: true,
		CollectorOverride:  rc,
		EngineOverride:     eng,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Should return without error even though we cancelled mid-ramp.
	_ = r.Run(ctx)

	if r.Phase() != PhaseDone {
		t.Errorf("final phase = %s, want %s", r.Phase(), PhaseDone)
	}
	if rc.written == nil {
		t.Errorf("WriteSummary not called even after cancel")
	}
}
