// Package handlers — scenarios template endpoint.
//
// Purpose:
//
//	Implements GET /api/v1/scenarios/templates by returning the hard-coded
//	template list from the mockdata package. The frontend uses this to
//	populate the "start from a template" picker in the new-run form.
//
// Related files:
//   - apps/api/internal/mockdata/mockdata.go (Templates())
//   - .orchestration/contracts/rest-api.md (GET /scenarios/templates)
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: response shape is part of the REST contract.
package handlers

import (
	"net/http"

	"github.com/Apextech-sys/radstorm/apps/api/internal/mockdata"
)

// Templates handles GET /api/v1/scenarios/templates.
func Templates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, mockdata.Templates())
}
