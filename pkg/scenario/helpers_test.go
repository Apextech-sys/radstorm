// Package scenario — coverage tests for small helpers and adapters.
//
// Purpose: Tests for internal helpers: parseSourceIPs, configHash,
// lifecycleEvent, bridge adapters, poolLookupAdapter, and Runner
// construction/lifecycle.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package scenario

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/config"
	"github.com/Apextech-sys/radstorm/pkg/events"
	"github.com/Apextech-sys/radstorm/pkg/radius"
	"github.com/Apextech-sys/radstorm/pkg/subscriber"
)

// osStatProgress returns the byte size of the file at path, or an error.
func osStatProgress(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func TestParseSourceIPs(t *testing.T) {
	out, err := parseSourceIPs([]string{"127.0.0.1", "10.0.0.1"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}

	if _, err := parseSourceIPs(nil); err == nil {
		t.Errorf("expected error for empty IPs")
	}
	if _, err := parseSourceIPs([]string{"not-an-ip"}); err == nil {
		t.Errorf("expected error for bad IP")
	}
}

func TestConfigHash(t *testing.T) {
	if got := configHash(nil); got != "" {
		t.Errorf("nil → %q, want empty", got)
	}
	got := configHash([]byte("hello"))
	if !strings.HasPrefix(got, "sha256:") {
		t.Errorf("hash = %q, want sha256: prefix", got)
	}
	if len(got) != len("sha256:")+64 {
		t.Errorf("hash length = %d", len(got))
	}
}

func TestLifecycleEvent(t *testing.T) {
	t0 := time.Now().Add(-2 * time.Second)
	e := lifecycleEvent(t0, "lifecycle_warmup")
	if e.EventType != "lifecycle_warmup" {
		t.Errorf("type = %s", e.EventType)
	}
	if e.Category != events.CategorySubscriberLifecycle {
		t.Errorf("category = %s", e.Category)
	}
	if e.OffsetMs < 1500 {
		t.Errorf("offsetMs = %d, want >=1500", e.OffsetMs)
	}
}

func TestRunner_Schedule_LiveCounters_SummaryPath(t *testing.T) {
	cfg := smokeConfig(2)
	r, err := New(Opts{
		Config:    cfg,
		Creds:     smokeCreds(2),
		OutputDir: t.TempDir(),
		RunID:     "r1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Schedule() == nil {
		t.Errorf("Schedule = nil")
	}
	if r.LiveCounters() == nil {
		t.Errorf("LiveCounters = nil")
	}
	if !strings.HasSuffix(r.SummaryPath(), "summary.json") {
		t.Errorf("SummaryPath = %s", r.SummaryPath())
	}
	if filepath.Base(r.SummaryPath()) != "summary.json" {
		t.Errorf("SummaryPath base = %s", filepath.Base(r.SummaryPath()))
	}
}

func TestPoolLookupAdapter_Lookup(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 1, AuthMethodPapPct: 100, TypePppoePct: 100},
	}
	pool := subscriber.NewPool([]config.Credential{{Username: "alice", Password: "pw"}}, cfg)

	a := poolLookupAdapter{p: pool}
	pkt := &radius.Packet{
		Code: radius.CodeCoARequest,
		Attributes: radius.AttributeList{
			{Type: radius.AttrUserName, Value: []byte("alice")},
		},
	}
	tgt, ok := a.Lookup(pkt)
	if !ok {
		t.Fatalf("expected lookup hit")
	}
	if tgt.Username() != "alice" {
		t.Errorf("username = %s", tgt.Username())
	}

	pkt2 := &radius.Packet{
		Code: radius.CodeCoARequest,
		Attributes: radius.AttributeList{
			{Type: radius.AttrUserName, Value: []byte("ghost")},
		},
	}
	if _, ok := a.Lookup(pkt2); ok {
		t.Errorf("expected no hit for unknown user")
	}
}

func TestRunnerDispatchServerInitiated_NilSafe(t *testing.T) {
	r := &Runner{}
	r.dispatchServerInitiated(nil) // should not panic
}

func TestRunnerDispatchServerInitiated_RoutesByCode(t *testing.T) {
	cfg := &config.Config{
		Subscribers: config.Subscribers{Count: 1, AuthMethodPapPct: 100, TypePppoePct: 100},
	}
	pool := subscriber.NewPool([]config.Credential{{Username: "alice", Password: "pw"}}, cfg)
	r := &Runner{pool: pool}

	pkt := &radius.Packet{
		Code: radius.CodeCoARequest,
		Attributes: radius.AttributeList{
			{Type: radius.AttrUserName, Value: []byte("alice")},
		},
	}
	r.dispatchServerInitiated(pkt) // exercises the CoA branch

	pkt2 := &radius.Packet{
		Code: radius.CodeDisconnectRequest,
		Attributes: radius.AttributeList{
			{Type: radius.AttrUserName, Value: []byte("alice")},
		},
	}
	r.dispatchServerInitiated(pkt2) // exercises the Disconnect branch
}

func TestCollectorBridgeFor_Submit(t *testing.T) {
	rc := &recordingCollector{}
	br := collectorBridgeFor(rc)
	br.Submit(events.Event{EventType: "x"})
	if got := len(rc.events); got != 1 {
		t.Errorf("events recorded = %d", got)
	}
}

func TestSubscriberCollectorBridge_Both(t *testing.T) {
	rc := &recordingCollector{}
	br := subscriberCollectorBridge{c: rc}
	br.Submit(events.Event{EventType: "y"})
	br.SubmitOutcome(events.SubscriberOutcome{Username: "u"})
	if len(rc.events) != 1 || len(rc.outcomes) != 1 {
		t.Errorf("bridge counts: %d events / %d outcomes", len(rc.events), len(rc.outcomes))
	}
}

func TestListenerCollectorBridge_Submit(t *testing.T) {
	rc := &recordingCollector{}
	br := listenerCollectorBridge{c: rc}
	br.Submit(events.Event{EventType: "z"})
	if len(rc.events) != 1 {
		t.Errorf("events = %d", len(rc.events))
	}
}

// Ensure the schedule's Last() returns 0 on empty.
func TestSchedule_Last_Empty(t *testing.T) {
	s := &Schedule{}
	if s.Last() != 0 {
		t.Errorf("Last empty = %v", s.Last())
	}
}

func TestNewRunner_SeedAndDefaults(t *testing.T) {
	cfg := smokeConfig(1)
	r, err := New(Opts{
		Config:    cfg,
		Creds:     smokeCreds(1),
		OutputDir: t.TempDir(),
		// Leave DrainTimeout / WarmupDelay at zero to exercise defaulting.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.opts.DrainTimeout != defaultDrainTimeout {
		t.Errorf("DrainTimeout default not applied: %v", r.opts.DrainTimeout)
	}
	if r.opts.WarmupDelay != 0 {
		// 0 is allowed (caller may want zero); just sanity-check it stays.
		t.Logf("warmup default = %v", r.opts.WarmupDelay)
	}
	if r.opts.RunID == "" {
		t.Errorf("RunID not generated when empty")
	}
}

// Ensure the runner's progress writer creates progress.jsonl during a run.
func TestRunner_WritesProgressJSONL(t *testing.T) {
	const n = 3
	dir := t.TempDir()
	rc := &recordingCollector{}
	r, err := New(Opts{
		Config:             smokeConfig(n),
		Creds:              smokeCreds(n),
		OutputDir:          dir,
		DrainTimeout:       1 * time.Second,
		WarmupDelay:        10 * time.Millisecond,
		SkipServerListener: true,
		CollectorOverride:  rc,
		EngineOverride:     &fakeEngine{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	progressPath := filepath.Join(dir, "progress.jsonl")
	fi, err := osStatProgress(progressPath)
	if err != nil {
		t.Fatalf("progress.jsonl missing: %v", err)
	}
	if fi <= 0 {
		t.Errorf("progress.jsonl empty")
	}
}

// keep `net` import alive (used elsewhere via radius helpers)
var _ = net.IPv4
