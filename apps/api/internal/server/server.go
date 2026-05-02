// Package server — HTTP server lifecycle.
//
// Purpose:
//
//	Wraps net/http.Server with graceful-shutdown semantics: ListenAndServe
//	in a goroutine, block on context cancellation, then call Shutdown with
//	a bounded timeout. Used by the cmd/radstorm-api main and by integration
//	tests.
//
// Related files:
//   - apps/api/cmd/radstorm-api/main.go
//   - apps/api/internal/server/router.go
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal — Run() blocks until ctx is cancelled.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// DefaultShutdownTimeout is the grace period given to in-flight requests
// during graceful shutdown.
const DefaultShutdownTimeout = 10 * time.Second

// Server is a wrapper around http.Server with logger plumbing.
type Server struct {
	http   *http.Server
	logger *slog.Logger
}

// New constructs a Server bound to the given address with the supplied
// handler.
func New(addr string, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		http: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			// SSE handlers stream for several seconds; keep ReadTimeout
			// generous and rely on per-handler context cancellation.
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 0, // 0 = no timeout, required for SSE
			IdleTimeout:  120 * time.Second,
		},
		logger: logger,
	}
}

// Run starts the server and blocks until ctx is cancelled (typically by a
// SIGTERM / SIGINT signal handler in main). On cancellation it issues a
// graceful Shutdown with DefaultShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("api_server_listening", slog.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("api_server_shutdown_requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("api_server_shutdown_error", slog.Any("err", err))
			return err
		}
		s.logger.Info("api_server_shutdown_complete")
		return nil
	}
}

// Addr returns the bound address — useful for tests that supply ":0" and
// then need to know which ephemeral port was chosen (callers that need that
// should switch to NewWithListener; this helper returns the configured value).
func (s *Server) Addr() string { return s.http.Addr }
