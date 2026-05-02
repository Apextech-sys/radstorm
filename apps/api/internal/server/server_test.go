// Package server — server lifecycle tests.
//
// Purpose:
//
//	Smoke-tests NewRouter end-to-end (health endpoint via the real chi
//	router with all middleware) and verifies New + Run support graceful
//	shutdown via context cancellation.
//
// Related files:
//   - apps/api/internal/server/router.go
//   - apps/api/internal/server/server.go
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal.
package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/radstorm/apps/api/internal/runs"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewRouter_HealthEndpoint(t *testing.T) {
	router := NewRouter(RouterDeps{Logger: quietLogger(), Store: runs.NewMemoryStore()})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestNewRouter_CORSPreflight(t *testing.T) {
	router := NewRouter(RouterDeps{Logger: quietLogger(), Store: runs.NewMemoryStore()})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/api/v1/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "http://localhost:3000", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestServer_GracefulShutdown(t *testing.T) {
	// Bind to an ephemeral port to avoid collisions.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	router := NewRouter(RouterDeps{Logger: quietLogger(), Store: runs.NewMemoryStore()})
	srv := New(addr, router, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- srv.Run(ctx) }()

	// Give the server a moment to start, then verify it responds.
	deadline := time.After(2 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		select {
		case <-deadline:
			t.Fatalf("server failed to start: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-doneCh:
		assert.NoError(t, err)
	case <-time.After(DefaultShutdownTimeout + 2*time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}
