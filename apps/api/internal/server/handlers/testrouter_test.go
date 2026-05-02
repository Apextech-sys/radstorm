// Package handlers — test helper that builds the production router.
//
// Purpose:
//
//	The handlers tests need a router with chi URL params wired up so they
//	can exercise routes like /runs/{id}/results. This file rebuilds the
//	production routes (mirroring server.NewRouter) to avoid an import cycle
//	between handlers and server.
//
// Related files:
//   - apps/api/internal/server/router.go (production router)
//   - apps/api/internal/server/handlers/handlers_test.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: test-only.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Apextech-sys/radstorm/apps/api/internal/runs"
)

// newTestRouter returns a chi router with the same routes as the production
// server, minus middleware. dataDir is used to anchor the events / results
// handlers; pass "" to use the default ("data").
func newTestRouter(store runs.Store, runner *runs.Runner, dataDir string) http.Handler {
	r := chi.NewRouter()

	runsHandler := NewRunsHandler(store, runner)
	eventsHandler := NewEventsHandler(store, dataDir)
	resultsHandler := NewResultsHandler(store, dataDir)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", Health)
		r.Get("/scenarios/templates", Templates)
		r.Route("/runs", func(r chi.Router) {
			r.Get("/", runsHandler.List)
			r.Post("/", runsHandler.Create)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", runsHandler.Get)
				r.Post("/cancel", runsHandler.Cancel)
				r.Get("/events", eventsHandler.Stream)
				r.Get("/results", resultsHandler.Summary)
				r.Get("/results/establishment-curve", resultsHandler.EstablishmentCurve)
				r.Get("/results/latency-histogram", resultsHandler.LatencyHistogram)
				r.Get("/artifacts", resultsHandler.Artifacts)
				r.Get("/artifacts/{name}", resultsHandler.Artifact)
			})
		})
	})

	return r
}
