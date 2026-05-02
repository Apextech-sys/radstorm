// Package runs — subprocess runner for the radstorm CLI.
//
// Purpose:
//
//	Spawns the `radstorm` CLI binary as a subprocess for each run, watches
//	the run's output directory for progress + summary, and updates the
//	store as the subprocess transitions through queued -> running ->
//	succeeded|failed|cancelled.
//
//	The runner is single-tenant: callers (handlers) have already gated on
//	Store.HasActive(). Cancellation is implemented by cancelling the
//	context passed to exec.CommandContext, which sends SIGTERM (or KILL on
//	Windows where graceful signals are not available).
//
// Related files:
//   - apps/api/internal/runs/store.go (Store interface + Run type)
//   - apps/api/internal/runs/sqlite_store.go (default store)
//   - apps/api/internal/server/handlers/runs.go (consumer)
//   - .orchestration/contracts/results-schema.md (summary.json shape)
//   - .orchestration/briefings/3b-api-full.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal Go API consumed by the runs handlers.
package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Apextech-sys/radstorm/pkg/config"
)

// Runner spawns the CLI subprocess for each run and watches its output.
//
// One Runner instance is shared by the API server. Because the API is
// single-tenant we only ever have at most one active subprocess in flight
// at a time, but the Runner does NOT enforce that itself — Store.Create's
// ErrConflict is the gate.
type Runner struct {
	store    Store
	logger   *slog.Logger
	dataDir  string // root for run directories: <dataDir>/runs/<id>/
	cliPath  string // resolved path to the radstorm binary
	cliArgsv []string

	mu        sync.Mutex
	active    map[string]*activeRun // runID -> handle
	shutdown  chan struct{}
	shutdownO sync.Once
}

// activeRun is the per-run state the Runner tracks while a subprocess is
// in flight.
type activeRun struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	runDir string
	done   chan struct{} // closed when the watcher goroutine returns
}

// Options configures NewRunner.
type Options struct {
	// DataDir is the root for run directories. <DataDir>/runs/<id>/ holds
	// config.toml, progress.jsonl, summary.json, run.log, and any artifacts
	// the CLI writes.
	DataDir string
	// CLIPath is the absolute path to the radstorm binary. If empty,
	// FindCLI is used at construction time.
	CLIPath string
	// Logger is used for runner-level diagnostics (subprocess start/exit,
	// orphan reaping, etc).
	Logger *slog.Logger
}

// NewRunner constructs a Runner. The data dir is created if missing. The
// CLI path is resolved if not provided.
func NewRunner(store Store, opts Options) (*Runner, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.DataDir == "" {
		opts.DataDir = "data"
	}
	if err := os.MkdirAll(filepath.Join(opts.DataDir, "runs"), 0o755); err != nil {
		return nil, fmt.Errorf("ensure data dir: %w", err)
	}
	cliPath := opts.CLIPath
	if cliPath == "" {
		p, err := FindCLI()
		if err != nil {
			return nil, fmt.Errorf("locate radstorm CLI: %w", err)
		}
		cliPath = p
	}
	abs, err := filepath.Abs(cliPath)
	if err == nil {
		cliPath = abs
	}
	return &Runner{
		store:    store,
		logger:   opts.Logger,
		dataDir:  opts.DataDir,
		cliPath:  cliPath,
		cliArgsv: []string{"run-scenario"},
		active:   make(map[string]*activeRun),
		shutdown: make(chan struct{}),
	}, nil
}

// FindCLI looks for the radstorm CLI binary in (in order):
//   - $RADSTORM_CLI_BIN if set
//   - ./bin/radstorm
//   - ./bin/radstorm.exe
//   - $PATH (via exec.LookPath)
//
// Returns ErrCLINotFound if none of those resolve.
func FindCLI() (string, error) {
	if env := os.Getenv("RADSTORM_CLI_BIN"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}
	candidates := []string{
		filepath.Join("bin", "radstorm"),
		filepath.Join("bin", "radstorm.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	if p, err := exec.LookPath("radstorm"); err == nil {
		return p, nil
	}
	return "", ErrCLINotFound
}

// ErrCLINotFound is returned by FindCLI when no radstorm binary is on disk
// or in $PATH.
var ErrCLINotFound = errors.New("radstorm CLI binary not found (set RADSTORM_CLI_BIN or place at ./bin/radstorm)")

// RunDir returns the directory <dataDir>/runs/<id>/.
func (r *Runner) RunDir(runID string) string {
	return filepath.Join(r.dataDir, "runs", runID)
}

// Start spawns the CLI subprocess for runID. The function returns once the
// subprocess has been started (status flipped to running) or an error has
// occurred during setup. A background goroutine continues watching the
// subprocess and updates the store on exit.
//
// Pre-condition: a queued Run with id=runID exists in the store.
func (r *Runner) Start(runID string) error {
	run, err := r.store.Get(runID)
	if err != nil {
		return fmt.Errorf("lookup run %s: %w", runID, err)
	}
	runDir := r.RunDir(runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}

	// Freeze the config to <runDir>/config.toml so the CLI sees exactly
	// what was POSTed.
	cfgPath := filepath.Join(runDir, "config.toml")
	if err := writeConfigTOML(cfgPath, run.Config); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}

	// Open run.log for the subprocess's stdout/stderr. The CLI writes its
	// own structured logs to its own files; this is a fallback capture.
	logPath := filepath.Join(runDir, "run.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create run.log: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	args := append([]string{}, r.cliArgsv...)
	args = append(args, "--config", cfgPath, "--out", runDir)
	cmd := exec.CommandContext(ctx, r.cliPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = mustGetwd()
	configurePlatformProc(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return fmt.Errorf("start CLI subprocess: %w", err)
	}

	r.mu.Lock()
	r.active[runID] = &activeRun{
		cmd:    cmd,
		cancel: cancel,
		runDir: runDir,
		done:   make(chan struct{}),
	}
	r.mu.Unlock()

	if _, err := r.store.UpdateStatus(runID, StatusRunning); err != nil {
		// Try to keep things consistent: kill the subprocess; the watcher
		// goroutine will drive the failed transition.
		r.logger.Error("runner_update_status_failed", slog.String("run_id", runID), slog.Any("err", err))
	}

	go r.watch(runID, cmd, cancel, logFile)
	return nil
}

// Cancel requests cancellation of an active run. Returns ErrNotFound if
// the run is not active.
func (r *Runner) Cancel(runID string) error {
	r.mu.Lock()
	a, ok := r.active[runID]
	r.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	a.cancel()
	return nil
}

// Shutdown signals all active runs to terminate, waits up to timeout for
// each subprocess to exit, then returns.
func (r *Runner) Shutdown(timeout time.Duration) {
	r.shutdownO.Do(func() { close(r.shutdown) })
	r.mu.Lock()
	handles := make([]*activeRun, 0, len(r.active))
	for _, a := range r.active {
		handles = append(handles, a)
	}
	r.mu.Unlock()

	for _, a := range handles {
		a.cancel()
	}
	deadline := time.After(timeout)
	for _, a := range handles {
		select {
		case <-a.done:
		case <-deadline:
			return
		}
	}
}

// watch waits for the subprocess to exit and reconciles store state.
func (r *Runner) watch(runID string, cmd *exec.Cmd, cancel context.CancelFunc, logFile *os.File) {
	defer func() {
		_ = logFile.Close()
		cancel()
		r.mu.Lock()
		if a, ok := r.active[runID]; ok {
			close(a.done)
			delete(r.active, runID)
		}
		r.mu.Unlock()
	}()

	waitErr := cmd.Wait()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	r.logger.Info("runner_subprocess_exit",
		slog.String("run_id", runID),
		slog.Int("exit_code", exitCode),
		slog.Any("err", waitErr),
	)

	// Whatever happened, try to attach the summary if it was written. The
	// CLI is expected to write summary.json even on failure; if it isn't
	// there we still record the final state.
	summaryPath := filepath.Join(r.RunDir(runID), "summary.json")
	if data, err := os.ReadFile(summaryPath); err == nil {
		var summary map[string]any
		if err := json.Unmarshal(data, &summary); err == nil {
			_, _ = r.store.SetSummary(runID, summary)
		}
	}

	// Determine final status. We trust the store's current status for
	// cancellation: if the user POSTed /cancel before this point, status
	// will be cancelling and we transition to cancelled regardless of
	// exit code.
	current, err := r.store.Get(runID)
	if err != nil {
		return
	}
	final := StatusFailed
	switch {
	case current.Status == StatusCancelling || current.Status == StatusCancelled:
		final = StatusCancelled
	case waitErr == nil && exitCode == 0:
		final = StatusSucceeded
	default:
		// Capture failure reason in progress for UI.
		p := current.Progress
		_, _ = r.store.UpdateProgress(runID, p)
	}
	_, _ = r.store.UpdateStatus(runID, final)
}

// writeConfigTOML serialises cfg to path as TOML.
func writeConfigTOML(path string, cfg *config.Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return nil
}

// mustGetwd returns os.Getwd() ignoring errors (cmd.Dir then defaults to
// the current process's working directory if Getwd ever fails).
func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
