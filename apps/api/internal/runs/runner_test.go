// Package runs — tests for the subprocess Runner.
//
// Purpose:
//
//	Drives the Runner end-to-end against the fake CLI binary at
//	testdata/fakecli, covering the success path (exit 0 -> succeeded with
//	a summary attached), the failure path (exit 1 -> failed), and the
//	cancellation path (FAKECLI_HANG=1 + Cancel -> cancelled). Also covers
//	FindCLI's fallback chain.
//
// Related files:
//   - apps/api/internal/runs/runner.go
//   - apps/api/internal/runs/testdata/fakecli/main.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal.
package runs

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quietLogger discards everything; runner-level logging is otherwise noisy
// in test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newRunnerWithFakeCLI builds a Runner backed by a fresh on-disk SQLite
// store and the test fake CLI. Returns the runner, the store, and the
// data directory so tests can poke around in run dirs if they want to.
func newRunnerWithFakeCLI(t *testing.T) (*Runner, Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dataDir, "runs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	r, err := NewRunner(store, Options{
		DataDir: dataDir,
		CLIPath: buildFakeCLI(t),
		Logger:  quietLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { r.Shutdown(2 * time.Second) })
	return r, store, dataDir
}

func TestRunner_Start_SuccessPath(t *testing.T) {
	t.Setenv("FAKECLI_PROGRESS_LINES", "2")
	t.Setenv("FAKECLI_SLEEP_MS", "10")
	r, store, dataDir := newRunnerWithFakeCLI(t)

	row, err := store.Create("", minimalConfigForRunner())
	require.NoError(t, err)
	require.NoError(t, r.Start(row.ID))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(row.ID)
		require.NoError(t, err)
		if got.Status == StatusSucceeded {
			require.NotNil(t, got.Summary)
			assert.Equal(t, "succeeded", got.Summary["outcome"])
			// progress.jsonl + summary.json + run.log + config.toml should
			// all exist on disk.
			runDir := filepath.Join(dataDir, "runs", row.ID)
			for _, f := range []string{"config.toml", "run.log", "progress.jsonl", "summary.json"} {
				_, err := os.Stat(filepath.Join(runDir, f))
				assert.NoError(t, err, "expected %s to exist", f)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run did not succeed in time")
}

func TestRunner_Start_FailurePath(t *testing.T) {
	t.Setenv("FAKECLI_PROGRESS_LINES", "1")
	t.Setenv("FAKECLI_SLEEP_MS", "5")
	t.Setenv("FAKECLI_FAIL", "1")
	r, store, _ := newRunnerWithFakeCLI(t)

	row, err := store.Create("", minimalConfigForRunner())
	require.NoError(t, err)
	require.NoError(t, r.Start(row.ID))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(row.ID)
		require.NoError(t, err)
		if got.Status == StatusFailed {
			// Summary exists with outcome=failed (the fake CLI writes one
			// even on failure).
			require.NotNil(t, got.Summary)
			assert.Equal(t, "failed", got.Summary["outcome"])
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run did not fail in time")
}

func TestRunner_Cancel_HangingSubprocess(t *testing.T) {
	t.Setenv("FAKECLI_HANG", "1")
	r, store, _ := newRunnerWithFakeCLI(t)

	row, err := store.Create("", minimalConfigForRunner())
	require.NoError(t, err)
	require.NoError(t, r.Start(row.ID))

	// Give the subprocess a moment to install its signal handler.
	time.Sleep(150 * time.Millisecond)

	// Flip status to cancelling like the handler would.
	_, err = store.UpdateStatus(row.ID, StatusCancelling)
	require.NoError(t, err)
	require.NoError(t, r.Cancel(row.ID))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.Get(row.ID)
		if got.Status == StatusCancelled {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := store.Get(row.ID)
	t.Fatalf("run did not reach cancelled in time; final status=%s", got.Status)
}

func TestRunner_Cancel_UnknownRun(t *testing.T) {
	r, _, _ := newRunnerWithFakeCLI(t)
	err := r.Cancel("does-not-exist")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRunner_Start_UnknownRun(t *testing.T) {
	r, _, _ := newRunnerWithFakeCLI(t)
	err := r.Start("does-not-exist")
	require.Error(t, err)
}

func TestRunner_Shutdown_StopsActive(t *testing.T) {
	t.Setenv("FAKECLI_HANG", "1")
	r, store, _ := newRunnerWithFakeCLI(t)

	row, err := store.Create("", minimalConfigForRunner())
	require.NoError(t, err)
	require.NoError(t, r.Start(row.ID))
	time.Sleep(150 * time.Millisecond)

	// Shutdown should kill the subprocess and return within the timeout.
	r.Shutdown(3 * time.Second)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.Get(row.ID)
		if got.Status == StatusFailed || got.Status == StatusSucceeded || got.Status == StatusCancelled {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := store.Get(row.ID)
	t.Fatalf("run did not reach a terminal state after shutdown; status=%s", got.Status)
}

func TestFindCLI_NotFound(t *testing.T) {
	// Point everything at a definitely-missing file.
	t.Setenv("RADSTORM_CLI_BIN", filepath.Join(t.TempDir(), "definitely-not-here"))
	// Move CWD to a temp dir so ./bin/radstorm doesn't accidentally exist.
	prev, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(prev) })
	require.NoError(t, os.Chdir(t.TempDir()))

	// Also wipe PATH so exec.LookPath fails.
	t.Setenv("PATH", "")

	_, err = FindCLI()
	assert.ErrorIs(t, err, ErrCLINotFound)
}

func TestFindCLI_RespectsEnvVar(t *testing.T) {
	// Create a stand-in binary file (just needs to exist for os.Stat).
	dir := t.TempDir()
	stub := filepath.Join(dir, "radstorm-stub")
	require.NoError(t, os.WriteFile(stub, []byte("stub"), 0o755))
	t.Setenv("RADSTORM_CLI_BIN", stub)

	got, err := FindCLI()
	require.NoError(t, err)
	assert.Equal(t, stub, got)
}

func TestRunDir_ReturnsExpectedPath(t *testing.T) {
	r, _, dataDir := newRunnerWithFakeCLI(t)
	got := r.RunDir("run-abc")
	assert.Equal(t, filepath.Join(dataDir, "runs", "run-abc"), got)
}

func TestNewRunner_CreatesDataDir(t *testing.T) {
	// NewRunner should ensure <DataDir>/runs/ exists, creating it if
	// missing. Use a non-default path inside t.TempDir to avoid Windows
	// file-handle races on test cleanup.
	dataDir := filepath.Join(t.TempDir(), "fresh")
	store, err := NewSQLiteStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	r, err := NewRunner(store, Options{
		DataDir: dataDir,
		CLIPath: buildFakeCLI(t),
		Logger:  quietLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { r.Shutdown(time.Second) })

	assert.Equal(t, dataDir, r.dataDir)
	_, err = os.Stat(filepath.Join(dataDir, "runs"))
	assert.NoError(t, err, "<DataDir>/runs should have been created")
}

func TestNewRunner_DefaultLogger(t *testing.T) {
	// With Logger=nil, NewRunner falls back to slog.Default(); just verify
	// construction doesn't panic.
	store, err := NewSQLiteStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	r, err := NewRunner(store, Options{
		DataDir: t.TempDir(),
		CLIPath: buildFakeCLI(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() { r.Shutdown(time.Second) })
	assert.NotNil(t, r.logger)
}
