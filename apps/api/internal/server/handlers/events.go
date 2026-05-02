// Package handlers — SSE events stream backed by file-tailing.
//
// Purpose:
//
//	Implements GET /api/v1/runs/{id}/events. Tails the run's
//	progress.jsonl file and re-emits each appended line as an SSE
//	`progress` event. When the run reaches a terminal status AND
//	summary.json exists on disk, emits a single `complete` event with
//	the summary contents, then closes the stream.
//
//	Heartbeats are sent every 15s as `event: ping` to keep idle
//	connections alive through proxies. If the client disconnects, the
//	tail goroutine terminates but the underlying CLI subprocess is
//	NOT killed.
//
// Related files:
//   - apps/api/internal/runs/tail.go (TailLines)
//   - apps/api/internal/runs/runner.go (writes progress.jsonl)
//   - apps/api/internal/runs/store.go (status source-of-truth)
//   - .orchestration/contracts/rest-api.md (GET /runs/{id}/events)
//   - .orchestration/contracts/results-schema.md (summary.json)
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: SSE format (`event:` + `data:` lines per event) is part of
// the REST contract; event names progress / log / ping / complete are
// fixed.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
)

// HeartbeatInterval is the cadence for SSE keep-alive pings. 15s is the
// briefing-mandated value; exposed so tests can shorten it.
var HeartbeatInterval = 15 * time.Second

// PollInterval is how often the SSE handler checks the store for status
// changes (so we know when to emit `complete`). Independent from the
// progress.jsonl tail interval.
var PollInterval = 250 * time.Millisecond

// EventsHandler bundles dependencies for the SSE stream.
type EventsHandler struct {
	Store   runs.Store
	DataDir string // root used to locate <DataDir>/runs/<id>/progress.jsonl
}

// NewEventsHandler constructs an EventsHandler. dataDir defaults to "data"
// when empty so unit tests that don't care about a real directory still work.
func NewEventsHandler(store runs.Store, dataDir string) *EventsHandler {
	if dataDir == "" {
		dataDir = "data"
	}
	return &EventsHandler{Store: store, DataDir: dataDir}
}

// Stream handles GET /api/v1/runs/{id}/events.
func (h *EventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.Store.Get(id)
	if err != nil {
		if errors.Is(err, runs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "run not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch run", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeSSE(w, "log", map[string]any{
		"offset_ms": 0,
		"level":     "info",
		"msg":       fmt.Sprintf("Streaming progress for %s", run.ID),
	})
	flusher.Flush()

	runDir := filepath.Join(h.DataDir, "runs", run.ID)
	progressPath := filepath.Join(runDir, "progress.jsonl")
	summaryPath := filepath.Join(runDir, "summary.json")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stopTail := make(chan struct{})
	progressCh := runs.TailLines(ctx, progressPath, runs.TailOptions{Stop: stopTail})

	heartbeat := time.NewTicker(HeartbeatInterval)
	defer heartbeat.Stop()
	poll := time.NewTicker(PollInterval)
	defer poll.Stop()

	defer close(stopTail)

	for {
		select {
		case <-ctx.Done():
			return

		case line, ok := <-progressCh:
			if !ok {
				// Tail terminated — usually because ctx was cancelled or
				// the file was unrecoverable. Fall through to status check.
				progressCh = nil
				continue
			}
			// Forward each progress line verbatim (it's already JSON).
			writeSSERaw(w, "progress", line)
			flusher.Flush()

		case <-heartbeat.C:
			// SSE comment line as a heartbeat — clients ignore it but
			// proxies see traffic. Use both an `event: ping` event and
			// a colon-prefixed comment for maximum compatibility.
			_, _ = fmt.Fprintf(w, ": heartbeat\nevent: ping\ndata: \n\n")
			flusher.Flush()

		case <-poll.C:
			cur, err := h.Store.Get(run.ID)
			if err != nil {
				return
			}
			if isTerminal(cur.Status) {
				// Wait briefly for summary.json to be flushed; the runner
				// reads it back into the store after the subprocess exits,
				// but the file may have been written milliseconds before
				// we observed the status flip.
				summary := cur.Summary
				if summary == nil {
					if data, err := os.ReadFile(summaryPath); err == nil {
						_ = json.Unmarshal(data, &summary)
					}
				}
				writeSSE(w, "complete", map[string]any{
					"run_id":  cur.ID,
					"status":  cur.Status,
					"summary": summary,
				})
				flusher.Flush()
				return
			}
		}
	}
}

// isTerminal reports whether a run status is final (no further events).
func isTerminal(s runs.Status) bool {
	switch s {
	case runs.StatusSucceeded, runs.StatusFailed, runs.StatusCancelled:
		return true
	}
	return false
}

// writeSSE writes one SSE event using the standard `event:` + `data:` form.
func writeSSE(w http.ResponseWriter, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
}

// writeSSERaw writes one SSE event with a pre-encoded JSON payload.
func writeSSERaw(w http.ResponseWriter, event string, payload []byte) {
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
}
