// Package http is the REST inbound adapter: the three read routes of
// apis/openapi.yaml on a chi router, RFC 9457 problem documents for every error
// and the liveness and readiness endpoints. It has no auth middleware (fleet
// decision 2026-09-11; internal/architecture's TestNoAuthMiddlewareReintroduced)
// and no write route, hence no Idempotency-Key.
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// Server holds the queries the handlers call.
type Server struct {
	Queries usecases.Queries
	// Readiness backs GET /readyz; nil means always ready (unit tests).
	Readiness *Readiness
}

// NewRouter builds the chi router for every endpoint of apis/openapi.yaml.
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	// A NUL (U+0000) in the path or the query is a uniform client error, never
	// a Postgres 22021 -> 500.
	r.Use(rejectNUL)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.Get("/products", s.handleSearch)
	r.Get("/products/{catalogItemId}", s.handleGetProduct)
	r.Get("/categories", s.handleListCategories)
	return r
}
