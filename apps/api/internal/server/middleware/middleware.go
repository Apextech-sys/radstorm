// Package middleware — HTTP middleware for the radstorm API server.
//
// Purpose:
//
//	Provides request-id assignment, structured-logging access logs, and a
//	panic recoverer that returns a JSON 500. CORS is configured separately
//	via go-chi/cors in router.go.
//
// Related files:
//   - apps/api/internal/server/router.go (wires these middlewares)
//   - apps/api/internal/server/server.go
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal — middleware signatures match net/http.Handler.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// requestIDKey is the context key under which the assigned request id is
// stored. Internal type to prevent collisions with other packages.
type requestIDKey struct{}

// RequestIDHeader is the canonical header used to propagate request ids.
const RequestIDHeader = "X-Request-ID"

// RequestID returns the request id stored in the context, or "" if none.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestIDMiddleware assigns an X-Request-ID header (honouring an existing
// inbound value) and stores it on the request context.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buf[:])
}

// statusRecorder captures the response status code so the access log can
// include it without forcing handlers to call WriteHeader.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush forwards Flush calls so chi's SSE writes flush correctly through the
// recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// LoggingMiddleware emits a single slog Info entry per request including
// method, path, status, duration, and request id.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			logger.Info("http_request",
				slog.String("request_id", RequestID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", r.RemoteAddr),
			)
		})
	}
}

// RecoverMiddleware converts panics into a 500 JSON response and logs the
// stack via slog. Without this, a panic would crash the entire server.
func RecoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic_recovered",
						slog.String("request_id", RequestID(r.Context())),
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error":      "internal server error",
						"request_id": RequestID(r.Context()),
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
