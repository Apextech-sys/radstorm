// Package handlers — runs lifecycle endpoints.
//
// Purpose:
//
//	Implements POST /runs (spawns the radstorm CLI subprocess via the
//	Runner), GET /runs (list), GET /runs/{id} (detail), and POST
//	/runs/{id}/cancel (sends cancellation through the Runner so the
//	subprocess receives SIGTERM/TerminateProcess).
//
// Related files:
//   - apps/api/internal/runs/store.go (state)
//   - apps/api/internal/runs/runner.go (subprocess supervisor)
//   - pkg/config/validate.go (validation)
//   - .orchestration/contracts/rest-api.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: response shapes per rest-api.md.
package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
)

// RunsHandler bundles dependencies for the runs endpoints.
type RunsHandler struct {
	Store  runs.Store
	Runner *runs.Runner // optional; if nil, Create returns 503 (used by some unit tests)
}

// NewRunsHandler constructs a RunsHandler. runner may be nil for tests
// that only exercise non-Create endpoints.
func NewRunsHandler(store runs.Store, runner *runs.Runner) *RunsHandler {
	return &RunsHandler{Store: store, Runner: runner}
}

// createRunRequest is the JSON body shape for POST /runs.
type createRunRequest struct {
	Name   string         `json:"name"`
	Config *config.Config `json:"config"`
}

// Create handles POST /api/v1/runs.
//
// Flow: validate -> Store.Create (returns 409 if already active) ->
// Runner.Start (spawns subprocess, transitions queued -> running). If
// Runner.Start fails we delete the freshly-created row to avoid leaving
// a permanently-queued ghost behind.
func (h *RunsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.Config == nil {
		writeError(w, http.StatusBadRequest, "config is required", nil)
		return
	}
	if err := config.Validate(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, "config validation failed", err.Error())
		return
	}
	if h.Runner == nil {
		writeError(w, http.StatusServiceUnavailable, "runner not configured", nil)
		return
	}
	run, err := h.Store.Create(req.Name, req.Config)
	if err != nil {
		if errors.Is(err, runs.ErrConflict) {
			writeError(w, http.StatusConflict, err.Error(), nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create run", err.Error())
		return
	}
	if err := h.Runner.Start(run.ID); err != nil {
		// Mark the run failed so HasActive() doesn't get stuck.
		_, _ = h.Store.UpdateStatus(run.ID, runs.StatusFailed)
		writeError(w, http.StatusInternalServerError, "failed to start subprocess", err.Error())
		return
	}
	// Re-read to pick up the StartedAt timestamp the Runner just set.
	if updated, err := h.Store.Get(run.ID); err == nil {
		run = updated
	}
	writeJSON(w, http.StatusCreated, run)
}

// List handles GET /api/v1/runs.
func (h *RunsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	status := r.URL.Query().Get("status")
	list := h.Store.List(limit, status)
	writeJSON(w, http.StatusOK, map[string]any{"runs": list})
}

// Get handles GET /api/v1/runs/{id}.
func (h *RunsHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, run)
}

// Cancel handles POST /api/v1/runs/{id}/cancel.
//
// Sends cancellation to the Runner (which kills the subprocess) and flips
// status to cancelling. The Runner's watcher goroutine is responsible for
// the final cancelling -> cancelled transition.
func (h *RunsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
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
	switch run.Status {
	case runs.StatusSucceeded, runs.StatusFailed, runs.StatusCancelled:
		writeError(w, http.StatusConflict, "run already finished", string(run.Status))
		return
	}
	updated, err := h.Store.UpdateStatus(id, runs.StatusCancelling)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel run", err.Error())
		return
	}
	// Best-effort cancellation through the Runner. If the runner doesn't
	// know about this id (e.g. recovered orphan), we still report 200 so
	// the UI sees the status flip.
	if h.Runner != nil {
		_ = h.Runner.Cancel(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     updated.ID,
		"status": updated.Status,
	})
}
