// Package http is the REST inbound adapter: the three read routes of
// apis/openapi.yaml on a chi router, RFC 9457 problem documents for every error
// and the liveness and readiness endpoints. It has no auth middleware (fleet
// decision 2026-09-11; internal/architecture's TestNoAuthMiddlewareReintroduced)
// and no write route, hence no Idempotency-Key.
package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/riandyrn/otelchi"
	otelchimetric "github.com/riandyrn/otelchi/metric"

	"github.com/IQVO/product-discovery/internal/adapters/inbound/requestlog"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// Server holds the queries the handlers call.
type Server struct {
	Queries usecases.Queries
	// Readiness backs GET /readyz; nil means always ready (unit tests).
	Readiness *Readiness
}

// DefaultServiceName names the server spans and the HTTP metrics when the
// composition root passes no service name.
const DefaultServiceName = "product-discovery"

// NewRouter builds the chi router for every endpoint of apis/openapi.yaml. A nil
// logger defaults to slog.Default(); an empty serviceName to DefaultServiceName.
//
// Middleware order is the fleet observability standard: RequestID, then otelchi
// (the server span, named after the chi route pattern, so one span name per
// route and not per catalogue item), then the request-duration histogram, then
// RequestLogger (it runs inside the span, so its record carries trace_id and
// span_id), then Recoverer. telemetry.Setup must have run before this call:
// otelchimetric binds the global MeterProvider when it is constructed.
func NewRouter(s *Server, logger *slog.Logger, serviceName string) http.Handler {
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(otelchi.Middleware(serviceName, otelchi.WithChiRoutes(r)))
	r.Use(otelchimetric.NewServerRequestDuration(otelchimetric.NewBaseConfig(serviceName)))
	r.Use(requestlog.RequestLogger(logger))
	r.Use(middleware.Recoverer)
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
