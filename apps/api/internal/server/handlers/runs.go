// Package handlers — runs lifecycle endpoints.
//
// Purpose:
//
//	Implements POST /runs (queue a fake run that "succeeds" 2s later),
//	GET /runs (list), GET /runs/{id} (detail), POST /runs/{id}/cancel.
//
//	The fake state machine is intentional: the frontend can drive its UI
//	end-to-end against this skeleton before the real CLI subprocess
//	supervisor lands in Wave 3.
//
// Related files:
//   - apps/api/internal/runs/store.go (state)
//   - pkg/config/validate.go (validation)
//   - apps/api/internal/mockdata/mockdata.go (canned summary)
//   - .orchestration/contracts/rest-api.md
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: response shapes per rest-api.md.
package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/mockdata"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
)

// FakeRunDuration controls how long the fake state machine takes to flip a
// queued run through running → succeeded. Exposed so tests can override it
// to keep the suite fast.
var FakeRunDuration = 2 * time.Second

// RunsHandler bundles dependencies for the runs endpoints. A shared instance
// is mounted on the router.
type RunsHandler struct {
	Store runs.Store
}

// NewRunsHandler constructs a RunsHandler.
func NewRunsHandler(store runs.Store) *RunsHandler {
	return &RunsHandler{Store: store}
}

// createRunRequest is the JSON body shape for POST /runs.
type createRunRequest struct {
	Name   string         `json:"name"`
	Config *config.Config `json:"config"`
}

// Create handles POST /api/v1/runs. It validates the config, rejects with
// 409 if a run is already active, and otherwise stores a queued run plus
// kicks off a goroutine that progresses the run through the fake lifecycle.
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
	run, err := h.Store.Create(req.Name, req.Config)
	if err != nil {
		if errors.Is(err, runs.ErrConflict) {
			writeError(w, http.StatusConflict, err.Error(), nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create run", err.Error())
		return
	}
	go h.progressFakeRun(run.ID)
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
	// Schedule final transition to cancelled.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = h.Store.UpdateStatus(id, runs.StatusCancelled)
	}()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     updated.ID,
		"status": updated.Status,
	})
}

// progressFakeRun walks a freshly-created run through the queued → running →
// succeeded transitions over FakeRunDuration. It also writes a few progress
// snapshots and finally attaches the canned summary so GET /runs/{id} and
// /results both return realistic-looking data.
func (h *RunsHandler) progressFakeRun(id string) {
	// Brief delay before starting so the queued state is observable.
	time.Sleep(200 * time.Millisecond)
	if r, _ := h.Store.Get(id); r != nil && r.Status == runs.StatusCancelled {
		return
	}
	if _, err := h.Store.UpdateStatus(id, runs.StatusRunning); err != nil {
		return
	}

	// Spread progress updates across FakeRunDuration.
	seq := mockdata.ProgressSequence()
	step := FakeRunDuration / time.Duration(len(seq)+1)
	if step < 50*time.Millisecond {
		step = 50 * time.Millisecond
	}
	for _, ev := range seq {
		time.Sleep(step)
		if r, _ := h.Store.Get(id); r == nil || r.Status == runs.StatusCancelled {
			return
		}
		_, _ = h.Store.UpdateProgress(id, runs.Progress{
			ElapsedMs:              int64(ev.OffsetMs),
			SubscribersTotal:       1000,
			SubscribersActivated:   ev.Activated,
			SubscribersEstablished: ev.Established,
			SubscribersFailed:      ev.Failed,
		})
	}

	// Final transition: if cancellation slipped in, leave it alone.
	r, _ := h.Store.Get(id)
	if r == nil {
		return
	}
	if r.Status == runs.StatusCancelling || r.Status == runs.StatusCancelled {
		_, _ = h.Store.UpdateStatus(id, runs.StatusCancelled)
		return
	}
	_, _ = h.Store.SetSummary(id, mockdata.Summary(id))
	_, _ = h.Store.UpdateStatus(id, runs.StatusSucceeded)
}
