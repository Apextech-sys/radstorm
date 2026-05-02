// Package handlers — health endpoint.
//
// Purpose:
//
//	Implements GET /api/v1/health. This is the one endpoint that is fully
//	"real" in the Wave 1 skeleton (no mocked data, no fake state machine).
//
// Related files:
//   - .orchestration/contracts/rest-api.md (GET /api/v1/health)
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: response shape `{"status":"ok","version":"…"}` is part of the
// REST contract.
package handlers

import "net/http"

// Version is the build version reported by /health. Wave 5 will wire this to
// a build-time ldflag; for the skeleton it is hard-coded.
const Version = "0.1.0-skeleton"

// Health handles GET /api/v1/health.
func Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": Version,
	})
}
