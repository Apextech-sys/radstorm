// Package server — HTTP router wiring.
//
// Purpose:
//
//	Builds the chi router, mounts every endpoint defined in
//	.orchestration/contracts/rest-api.md, and applies the request-id /
//	logging / recover / CORS middleware stack. Routes are constructed in
//	one place so the OpenAPI spec at apps/api/openapi.yaml can be diffed
//	against this file in code review.
//
// Related files:
//   - apps/api/openapi.yaml
//   - apps/api/internal/server/handlers/*
//   - apps/api/internal/server/middleware/middleware.go
//   - .orchestration/contracts/rest-api.md
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: routes here MUST match openapi.yaml; both MUST match
// rest-api.md.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/server/handlers"
	mw "github.com/Apextech-sys/reflex-radstorm/apps/api/internal/server/middleware"
)

// NewRouter wires up the full API surface. The store is injected so tests
// can supply a fresh in-memory store per test.
func NewRouter(logger *slog.Logger, store runs.Store) http.Handler {
	r := chi.NewRouter()

	// Middleware ordering: request-id first so logger and recover both have
	// access to it; recover wraps everything else; logging is innermost so
	// it sees the final response status from the handler.
	r.Use(mw.RequestIDMiddleware)
	r.Use(mw.RecoverMiddleware(logger))
	r.Use(mw.LoggingMiddleware(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", mw.RequestIDHeader},
		ExposedHeaders:   []string{mw.RequestIDHeader},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	runsHandler := handlers.NewRunsHandler(store)
	eventsHandler := handlers.NewEventsHandler(store)
	resultsHandler := handlers.NewResultsHandler(store)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", handlers.Health)

		r.Get("/scenarios/templates", handlers.Templates)

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
