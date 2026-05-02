// Package handlers — results endpoints.
//
// Purpose:
//
//	Implements GET /api/v1/runs/{id}/results, …/establishment-curve,
//	…/latency-histogram, …/artifacts, and …/artifacts/{name}. All return
//	canned data from the mockdata package; Wave 3 will read these from the
//	real CLI subprocess output.
//
// Related files:
//   - apps/api/internal/mockdata/mockdata.go
//   - apps/api/internal/runs/store.go
//   - .orchestration/contracts/rest-api.md
//   - .orchestration/contracts/results-schema.md
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: response shapes per rest-api.md / results-schema.md.
package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/mockdata"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
)

// ResultsHandler bundles dependencies for the results endpoints.
type ResultsHandler struct {
	Store runs.Store
}

// NewResultsHandler constructs a ResultsHandler.
func NewResultsHandler(store runs.Store) *ResultsHandler {
	return &ResultsHandler{Store: store}
}

// requireRun looks up a run id and writes a 404/500 if missing. Returns nil
// on failure (caller should return immediately).
func (h *ResultsHandler) requireRun(w http.ResponseWriter, r *http.Request) *runs.Run {
	id := chi.URLParam(r, "id")
	run, err := h.Store.Get(id)
	if err != nil {
		if errors.Is(err, runs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "run not found", nil)
			return nil
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch run", err.Error())
		return nil
	}
	return run
}

// Summary handles GET /api/v1/runs/{id}/results.
func (h *ResultsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	run := h.requireRun(w, r)
	if run == nil {
		return
	}
	if run.Summary != nil {
		writeJSON(w, http.StatusOK, run.Summary)
		return
	}
	// Run still in flight — return canned summary so the frontend can be
	// developed against a stable shape. Wave 3 will return 409 here.
	writeJSON(w, http.StatusOK, mockdata.Summary(run.ID))
}

// EstablishmentCurve handles GET /api/v1/runs/{id}/results/establishment-curve.
func (h *ResultsHandler) EstablishmentCurve(w http.ResponseWriter, r *http.Request) {
	if h.requireRun(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, mockdata.EstablishmentCurve())
}

// LatencyHistogram handles GET /api/v1/runs/{id}/results/latency-histogram.
func (h *ResultsHandler) LatencyHistogram(w http.ResponseWriter, r *http.Request) {
	if h.requireRun(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, mockdata.LatencyHistogram())
}

// Artifacts handles GET /api/v1/runs/{id}/artifacts. The skeleton returns an
// empty list; Wave 3 will populate this from the CLI output directory.
func (h *ResultsHandler) Artifacts(w http.ResponseWriter, r *http.Request) {
	if h.requireRun(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": []any{}})
}

// Artifact handles GET /api/v1/runs/{id}/artifacts/{name}. The skeleton has
// no real artifacts, so this always 404s; the route is wired now so the
// frontend can build hrefs without breakage.
func (h *ResultsHandler) Artifact(w http.ResponseWriter, r *http.Request) {
	if h.requireRun(w, r) == nil {
		return
	}
	name := chi.URLParam(r, "name")
	writeError(w, http.StatusNotFound, "artifact not available in skeleton", name)
}
