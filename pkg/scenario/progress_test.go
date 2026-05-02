// Package scenario — progress writer tests.
//
// Purpose: Tests for the JSONL progress writer that streams live counters
// during a scenario run to progress.jsonl on disk.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package scenario

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProgressWriter_EmitsSnapshots(t *testing.T) {
	dir := t.TempDir()
	cnt := newLiveCounters()
	cnt.activated.Store(10)
	cnt.established.Store(7)
	cnt.failed.Store(1)
	cnt.retransmits.Store(3)

	pw, err := newProgressWriter(progressOpts{
		OutputDir: dir,
		T0:        time.Now(),
		Counters:  cnt,
		Tick:      30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("newProgressWriter: %v", err)
	}
	if pw.Path() != filepath.Join(dir, "progress.jsonl") {
		t.Errorf("Path = %s", pw.Path())
	}

	ctx, cancel := context.WithCancel(context.Background())
	pw.Start(ctx)
	time.Sleep(120 * time.Millisecond)
	cnt.setPhase(PhaseRamp)
	time.Sleep(120 * time.Millisecond)
	cancel()
	pw.Stop()

	f, err := os.Open(pw.Path())
	if err != nil {
		t.Fatalf("open progress.jsonl: %v", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var snaps []Snapshot
	for scanner.Scan() {
		var s Snapshot
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			t.Fatalf("unmarshal: %v (line=%q)", err, scanner.Text())
		}
		snaps = append(snaps, s)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(snaps) < 2 {
		t.Fatalf("got %d snapshots, want at least 2", len(snaps))
	}
	if snaps[0].Activated != 10 || snaps[0].Established != 7 {
		t.Errorf("first snapshot: %+v", snaps[0])
	}
	if snaps[0].InFlight != 2 {
		t.Errorf("inFlight = %d, want 2 (10 - 7 - 1)", snaps[0].InFlight)
	}
	// Verify a phase transition appears at some point.
	sawRamp := false
	for _, s := range snaps {
		if s.Phase == string(PhaseRamp) {
			sawRamp = true
		}
	}
	if !sawRamp {
		t.Errorf("expected a ramp-phase snapshot among %d", len(snaps))
	}
}

func TestProgressWriter_RequiresOutputDir(t *testing.T) {
	_, err := newProgressWriter(progressOpts{Counters: newLiveCounters()})
	if err == nil {
		t.Fatalf("expected error when OutputDir empty")
	}
}

func TestProgressWriter_RequiresCounters(t *testing.T) {
	_, err := newProgressWriter(progressOpts{OutputDir: t.TempDir()})
	if err == nil {
		t.Fatalf("expected error when Counters nil")
	}
}

func TestLiveCounters_InFlight_NeverNegative(t *testing.T) {
	c := newLiveCounters()
	c.established.Store(5)
	c.failed.Store(5)
	if c.inFlight() != 0 {
		t.Errorf("inFlight = %d, want 0 (clamped)", c.inFlight())
	}
}
