// Package handlers — HTTP handlers for the radstorm API.
//
// Purpose:
//
//	Shared JSON write/read helpers and the common error envelope used by
//	every handler. Per-endpoint handler files live alongside this file.
//
// Related files:
//   - apps/api/internal/server/router.go (mounts these handlers)
//   - .orchestration/contracts/rest-api.md
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: error envelope shape is part of the REST contract.
package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ErrorBody is the JSON envelope returned for non-2xx responses. The
// `details` field is optional and used for validation errors (a list of
// {field, message} objects).
type ErrorBody struct {
	Error   string `json:"error"`
	Details any    `json:"details,omitempty"`
}

// writeJSON writes v as JSON with the given status code. Any encoding error
// is logged-via-panic so the recover middleware turns it into a 500.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Connection likely closed mid-write; nothing useful we can do here.
		return
	}
}

// writeError writes the standard error envelope.
func writeError(w http.ResponseWriter, status int, msg string, details any) {
	writeJSON(w, status, ErrorBody{Error: msg, Details: details})
}

// readJSON decodes the request body into v. Returns a friendly error string
// suitable for surfacing in a 400.
func readJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("request body is empty")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}
