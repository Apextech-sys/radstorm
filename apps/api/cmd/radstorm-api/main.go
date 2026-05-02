// Command radstorm-api — HTTP API server for the radstorm test harness.
//
// Purpose:
//
//	Boots the server with an in-memory run store, registers a SIGINT/SIGTERM
//	handler that triggers graceful shutdown, and reads the bind address from
//	$RADSTORM_API_ADDR (default ":8080"). In Wave 3 the in-memory store is
//	swapped for SQLite-backed persistence and a CLI-subprocess supervisor.
//
// Related files:
//   - apps/api/internal/server/server.go (lifecycle)
//   - apps/api/internal/server/router.go (routes)
//   - apps/api/internal/runs/store.go (state)
//   - .orchestration/contracts/rest-api.md
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: command-line entry point; env vars:
//   - RADSTORM_API_ADDR: bind address (default ":8080")
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/server"
)

// envAddr is the env var that overrides the bind address.
const envAddr = "RADSTORM_API_ADDR"

// defaultAddr is used when $RADSTORM_API_ADDR is unset or empty.
const defaultAddr = ":8080"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	addr := os.Getenv(envAddr)
	if addr == "" {
		addr = defaultAddr
	}

	store := runs.NewMemoryStore()
	router := server.NewRouter(logger, store)
	srv := server.New(addr, router, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info("radstorm_api_starting",
		slog.String("addr", addr),
		slog.String("env_addr", envAddr),
	)

	if err := srv.Run(ctx); err != nil {
		logger.Error("radstorm_api_exit_error", slog.Any("err", err))
		os.Exit(1)
	}
	logger.Info("radstorm_api_exit_clean")
}
