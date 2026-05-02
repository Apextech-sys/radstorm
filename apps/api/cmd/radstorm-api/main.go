// Command radstorm-api — HTTP API server for the radstorm test harness.
//
// Purpose:
//
//	Boots the server with a SQLite-backed run store, a subprocess runner
//	that spawns the radstorm CLI for each run, and registers a
//	SIGINT/SIGTERM handler that triggers graceful shutdown of both the
//	HTTP server and any in-flight subprocess. Configuration comes from the
//	following environment variables:
//
//	  - RADSTORM_API_ADDR  : bind address (default ":8080")
//	  - RADSTORM_DATA_DIR  : root for runs.db + per-run output dirs
//	                          (default "data")
//	  - RADSTORM_CLI_BIN   : absolute path to the radstorm CLI binary
//	                          (default: bin/radstorm[.exe] or $PATH lookup)
//
// Related files:
//   - apps/api/internal/server/server.go (lifecycle)
//   - apps/api/internal/server/router.go (routes)
//   - apps/api/internal/runs/sqlite_store.go (state)
//   - apps/api/internal/runs/runner.go (subprocess supervisor)
//   - .orchestration/contracts/rest-api.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: command-line entry point. Env vars listed above.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Apextech-sys/radstorm/apps/api/internal/runs"
	"github.com/Apextech-sys/radstorm/apps/api/internal/server"
)

// Environment variable names recognised by the server.
const (
	envAddr    = "RADSTORM_API_ADDR"
	envDataDir = "RADSTORM_DATA_DIR"
	envCLIBin  = "RADSTORM_CLI_BIN"
)

// Default configuration when the corresponding env var is unset.
const (
	defaultAddr    = ":8080"
	defaultDataDir = "data"
)

// runnerShutdownTimeout caps how long we wait for in-flight subprocesses
// to exit during graceful shutdown before main returns.
var runnerShutdownTimeout = 10 * time.Second

func main() {
	// Handle --version / -v / version flag without booting the server.
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" || a == "version" {
			_, _ = os.Stdout.WriteString("radstorm-api version " + Version + "\n")
			return
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	addr := os.Getenv(envAddr)
	if addr == "" {
		addr = defaultAddr
	}
	dataDir := os.Getenv(envDataDir)
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		logger.Error("radstorm_api_data_dir_failed",
			slog.String("data_dir", dataDir), slog.Any("err", err))
		os.Exit(1)
	}

	dbPath := filepath.Join(dataDir, "runs.db")
	store, err := runs.NewSQLiteStore(dbPath)
	if err != nil {
		logger.Error("radstorm_api_store_failed",
			slog.String("db_path", dbPath), slog.Any("err", err))
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	runner, err := runs.NewRunner(store, runs.Options{
		DataDir: dataDir,
		CLIPath: os.Getenv(envCLIBin),
		Logger:  logger,
	})
	if err != nil {
		// CLI not found is fatal in production. (Tests construct their own
		// runner with an explicit path to the fake CLI.)
		logger.Error("radstorm_api_runner_failed",
			slog.String("cli_bin_env", envCLIBin), slog.Any("err", err))
		os.Exit(1)
	}

	router := server.NewRouter(server.RouterDeps{
		Logger:  logger,
		Store:   store,
		Runner:  runner,
		DataDir: dataDir,
	})
	srv := server.New(addr, router, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info("radstorm_api_starting",
		slog.String("version", Version),
		slog.String("addr", addr),
		slog.String("data_dir", dataDir),
		slog.String("db_path", dbPath),
	)

	runErr := srv.Run(ctx)

	// Always attempt to drain in-flight subprocesses before returning.
	runner.Shutdown(runnerShutdownTimeout)

	if runErr != nil {
		logger.Error("radstorm_api_exit_error", slog.Any("err", runErr))
		os.Exit(1)
	}
	logger.Info("radstorm_api_exit_clean")
}
