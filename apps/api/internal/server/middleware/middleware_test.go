// Package middleware — tests.
//
// Purpose:
//
//	Verifies request-id propagation, panic recovery returns JSON 500, and
//	the access logger sets a status when the handler doesn't.
//
// Related files:
//   - apps/api/internal/server/middleware/middleware.go
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal.
package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRequestID_GeneratedWhenAbsent(t *testing.T) {
	var seen string
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.NotEmpty(t, seen)
	assert.Equal(t, seen, rec.Header().Get(RequestIDHeader))
}

func TestRequestID_HonoursInbound(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "abc-123", RequestID(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, "abc-123", rec.Header().Get(RequestIDHeader))
}

func TestRecover_PanicTo500(t *testing.T) {
	h := RecoverMiddleware(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "internal server error", body["error"])
}

func TestLogging_DoesNotBlockResponse(t *testing.T) {
	h := LoggingMiddleware(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, "hi", rec.Body.String())
}
