// Package runs — tests for TailLines.
//
// Purpose:
//
//	Verifies the JSONL file tailer: file-doesn't-exist-yet quiet polling,
//	incremental line delivery as content is appended, and clean shutdown
//	via context cancellation or the Stop channel.
//
// Related files:
//   - apps/api/internal/runs/tail.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal.
package runs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailLines_DeliversAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.jsonl")
	// Pre-create the file so the tailer doesn't have to wait.
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := TailLines(ctx, path, TailOptions{Interval: 20 * time.Millisecond})

	// Append three lines with small gaps; tailer should emit each.
	go func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Errorf("open append: %v", err)
			return
		}
		defer f.Close()
		for i := 0; i < 3; i++ {
			_, _ = f.WriteString("line-" + string(rune('a'+i)) + "\n")
			_ = f.Sync()
			time.Sleep(30 * time.Millisecond)
		}
	}()

	got := make([]string, 0, 3)
	deadline := time.After(3 * time.Second)
	for len(got) < 3 {
		select {
		case line, ok := <-out:
			if !ok {
				t.Fatalf("channel closed early; got=%v", got)
			}
			got = append(got, string(line))
		case <-deadline:
			t.Fatalf("did not receive 3 lines in time; got=%v", got)
		}
	}
	assert.Equal(t, []string{"line-a", "line-b", "line-c"}, got)
}

func TestTailLines_FileDoesNotExistYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-yet.jsonl")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := TailLines(ctx, path, TailOptions{Interval: 20 * time.Millisecond})

	// Wait a bit, then create the file with one line.
	time.Sleep(80 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("hello\n"), 0o644))

	select {
	case line := <-out:
		assert.Equal(t, "hello", string(line))
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive line after file appeared")
	}
}

func TestTailLines_StopChannel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan struct{})
	out := TailLines(ctx, path, TailOptions{Interval: 10 * time.Millisecond, Stop: stop})

	close(stop)
	// Channel should close shortly after stop fires.
	select {
	case _, ok := <-out:
		if ok {
			// Drain until close.
			for range out {
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("output channel did not close after stop signalled")
	}
}

func TestTailLines_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	out := TailLines(ctx, path, TailOptions{Interval: 10 * time.Millisecond})
	cancel()

	select {
	case _, ok := <-out:
		if ok {
			for range out {
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("output channel did not close after context cancelled")
	}
}

func TestTailLines_Truncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.jsonl")
	// Start with a relatively long line so the second (shorter) write
	// is unambiguously a truncation (smaller file size on stat).
	require.NoError(t, os.WriteFile(path, []byte("first-line-longer-than-the-replacement\n"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := TailLines(ctx, path, TailOptions{Interval: 20 * time.Millisecond})

	// Receive the first line.
	select {
	case line := <-out:
		assert.Equal(t, "first-line-longer-than-the-replacement", string(line))
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive first line")
	}

	// Replace with a shorter content. Stat will report a smaller size,
	// triggering the tailer's reopen-from-zero path.
	require.NoError(t, os.WriteFile(path, []byte("after\n"), 0o644))
	select {
	case line := <-out:
		assert.Equal(t, "after", string(line))
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive line after truncation")
	}
}

func TestTailLines_DefaultInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("only-line\n"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Interval=0 should pick DefaultTailInterval (200ms).
	out := TailLines(ctx, path, TailOptions{})
	select {
	case line := <-out:
		assert.Equal(t, "only-line", string(line))
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive line with default interval")
	}
}
