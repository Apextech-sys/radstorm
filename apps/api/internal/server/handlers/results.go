// Package handlers — results endpoints backed by run-directory files.
//
// Purpose:
//
//	Implements GET /api/v1/runs/{id}/results, …/establishment-curve,
//	…/latency-histogram, …/artifacts, and …/artifacts/{name}. Each
//	endpoint reads from <DataDir>/runs/<id>/ on disk; for in-flight runs
//	without a summary yet, returns 409 Conflict.
//
//	The establishment-curve and latency-histogram endpoints derive their
//	response from summary.json (the curve lives at
//	establishment.curve, the histogram is computed from the latency
//	percentiles).
//
// Related files:
//   - apps/api/internal/runs/runner.go (writes summary.json into runDir)
//   - apps/api/internal/runs/store.go
//   - .orchestration/contracts/rest-api.md
//   - .orchestration/contracts/results-schema.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: response shapes per rest-api.md / results-schema.md.
package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
)

// ResultsHandler bundles dependencies for the results endpoints.
type ResultsHandler struct {
	Store   runs.Store
	DataDir string
}

// NewResultsHandler constructs a ResultsHandler. dataDir defaults to
// "data" when empty.
func NewResultsHandler(store runs.Store, dataDir string) *ResultsHandler {
	if dataDir == "" {
		dataDir = "data"
	}
	return &ResultsHandler{Store: store, DataDir: dataDir}
}

// requireRun looks up a run id and writes a 404/500 if missing.
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

// runDir returns the on-disk directory for the given run.
func (h *ResultsHandler) runDir(runID string) string {
	return filepath.Join(h.DataDir, "runs", runID)
}

// loadSummary returns the summary either from the store (preferred, since
// the runner already parsed and persisted it) or by reading summary.json
// from the run directory. Returns (nil, nil) when no summary is available.
func (h *ResultsHandler) loadSummary(run *runs.Run) (map[string]any, error) {
	if run.Summary != nil {
		return run.Summary, nil
	}
	path := filepath.Join(h.runDir(run.ID), "summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var summary map[string]any
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// Summary handles GET /api/v1/runs/{id}/results.
func (h *ResultsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	run := h.requireRun(w, r)
	if run == nil {
		return
	}
	summary, err := h.loadSummary(run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read summary", err.Error())
		return
	}
	if summary == nil {
		writeError(w, http.StatusConflict, "results not available yet", string(run.Status))
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// EstablishmentCurve handles GET /api/v1/runs/{id}/results/establishment-curve.
//
// Reads the curve out of summary.json:establishment.curve and wraps it in
// the response envelope rest-api.md mandates.
func (h *ResultsHandler) EstablishmentCurve(w http.ResponseWriter, r *http.Request) {
	run := h.requireRun(w, r)
	if run == nil {
		return
	}
	summary, err := h.loadSummary(run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read summary", err.Error())
		return
	}
	if summary == nil {
		writeError(w, http.StatusConflict, "results not available yet", string(run.Status))
		return
	}
	curve := extractCurve(summary)
	writeJSON(w, http.StatusOK, map[string]any{
		"interval_ms": 1000,
		"points":      curve,
	})
}

// extractCurve digs summary.establishment.curve out of the parsed map.
// Returns an empty slice when the path doesn't resolve so the response is
// still well-formed.
func extractCurve(summary map[string]any) []any {
	est, ok := summary["establishment"].(map[string]any)
	if !ok {
		return []any{}
	}
	curve, ok := est["curve"].([]any)
	if !ok {
		return []any{}
	}
	return curve
}

// LatencyHistogram handles GET /api/v1/runs/{id}/results/latency-histogram.
//
// Computes a small set of buckets from summary.establishment.latency_ms
// percentiles. This isn't a true histogram (we don't have per-event
// latencies in summary.json) but it matches the contract's bucket shape
// and is enough for the UI's chart.
func (h *ResultsHandler) LatencyHistogram(w http.ResponseWriter, r *http.Request) {
	run := h.requireRun(w, r)
	if run == nil {
		return
	}
	summary, err := h.loadSummary(run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read summary", err.Error())
		return
	}
	if summary == nil {
		writeError(w, http.StatusConflict, "results not available yet", string(run.Status))
		return
	}
	buckets := extractHistogram(summary)
	writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets})
}

// extractHistogram synthesises bucket counts from latency percentiles plus
// the established subscriber count.
func extractHistogram(summary map[string]any) []map[string]any {
	est, _ := summary["establishment"].(map[string]any)
	subs, _ := summary["subscribers"].(map[string]any)
	totalEstablished := asInt(subs["established"])
	if totalEstablished == 0 {
		// fall back to subscribers.total
		totalEstablished = asInt(subs["total"])
	}
	if est == nil || totalEstablished == 0 {
		return []map[string]any{}
	}
	lat, _ := est["latency_ms"].(map[string]any)
	if lat == nil {
		return []map[string]any{}
	}
	// Use p50/p95/p99/max as bucket boundaries; cumulative counts derived
	// from the percentile semantics.
	type pct struct {
		key   string
		ratio float64
	}
	pcts := []pct{
		{"p50", 0.50},
		{"p95", 0.95},
		{"p99", 0.99},
		{"max", 1.00},
	}
	out := make([]map[string]any, 0, len(pcts))
	for _, p := range pcts {
		v, ok := lat[p.key]
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"le_ms": v,
			"count": int(float64(totalEstablished) * p.ratio),
		})
	}
	// Ensure ascending by le_ms.
	sort.SliceStable(out, func(i, j int) bool {
		return asFloat(out[i]["le_ms"]) < asFloat(out[j]["le_ms"])
	})
	return out
}

// Artifacts handles GET /api/v1/runs/{id}/artifacts.
//
// Lists every regular file in the run directory. Each entry's URL points
// back to the artifact-download endpoint.
func (h *ResultsHandler) Artifacts(w http.ResponseWriter, r *http.Request) {
	run := h.requireRun(w, r)
	if run == nil {
		return
	}
	dir := h.runDir(run.ID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, map[string]any{"artifacts": []any{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list artifacts", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"name": e.Name(),
			"size": info.Size(),
			"url":  "/api/v1/runs/" + run.ID + "/artifacts/" + e.Name(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": out})
}

// Artifact handles GET /api/v1/runs/{id}/artifacts/{name}.
//
// Streams the file contents back. Path traversal is blocked by rejecting
// names that contain a path separator or "..".
func (h *ResultsHandler) Artifact(w http.ResponseWriter, r *http.Request) {
	run := h.requireRun(w, r)
	if run == nil {
		return
	}
	name := chi.URLParam(r, "name")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		writeError(w, http.StatusBadRequest, "invalid artifact name", name)
		return
	}
	path := filepath.Join(h.runDir(run.ID), name)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "artifact not found", name)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to open artifact", err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stat artifact", err.Error())
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(name))
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if info.Size() > 0 {
		w.Header().Set("Content-Length", strFormatInt(info.Size()))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// contentTypeFor returns a sensible MIME type for an artifact filename.
func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".jsonl"):
		return "application/json"
	case strings.HasSuffix(name, ".log"):
		return "text/plain; charset=utf-8"
	case strings.HasSuffix(name, ".toml"):
		return "application/toml"
	case strings.HasSuffix(name, ".parquet"):
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

// asInt coerces an unknown JSON-decoded value into int.
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// asFloat coerces an unknown JSON-decoded value into float64.
func asFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

// strFormatInt is a tiny strconv-free int64-to-string helper to keep this
// file's imports small (we already import strings).
func strFormatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var (
		buf  [20]byte
		i    = len(buf)
		neg  bool
	)
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
