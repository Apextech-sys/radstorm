// Package handlers — SSE events stream.
//
// Purpose:
//
//	Implements GET /api/v1/runs/{id}/events. Emits a short sequence of
//	`progress` events followed by a single `complete` event carrying the
//	canned summary, then closes the stream. In Wave 3 the implementation
//	will tail progress.jsonl from the CLI subprocess; the over-the-wire
//	event format is contractual.
//
// Related files:
//   - apps/api/internal/mockdata/mockdata.go (ProgressSequence, Summary)
//   - apps/api/internal/runs/store.go
//   - .orchestration/contracts/rest-api.md (GET /runs/{id}/events)
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: SSE format (`event:` + `data:` lines per event) is part of the
// REST contract.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/mockdata"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
)

// SSEStepInterval controls the gap between fake progress events. Exposed so
// tests can shrink it.
var SSEStepInterval = 250 * time.Millisecond

// EventsHandler bundles dependencies for the SSE stream.
type EventsHandler struct {
	Store runs.Store
}

// NewEventsHandler constructs an EventsHandler.
func NewEventsHandler(store runs.Store) *EventsHandler {
	return &EventsHandler{Store: store}
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

	// Initial info log so the frontend has something to show immediately.
	writeSSE(w, "log", map[string]any{
		"offset_ms": 0,
		"level":     "info",
		"msg":       fmt.Sprintf("Streaming progress for %s", run.ID),
	})
	flusher.Flush()

	ctx := r.Context()
	for _, ev := range mockdata.ProgressSequence() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(SSEStepInterval):
		}
		writeSSE(w, "progress", map[string]any{
			"offset_ms":   ev.OffsetMs,
			"activated":   ev.Activated,
			"established": ev.Established,
			"failed":      ev.Failed,
			"in_flight":   ev.InFlight,
			"retransmits": ev.Retransmits,
		})
		flusher.Flush()
	}

	writeSSE(w, "complete", map[string]any{
		"summary": mockdata.Summary(run.ID),
	})
	flusher.Flush()
}

// writeSSE writes one SSE event using the standard `event:` + `data:` form.
// JSON encoding errors are silently dropped — the stream is best-effort and
// the client will simply close on the missing event.
func writeSSE(w http.ResponseWriter, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
}
