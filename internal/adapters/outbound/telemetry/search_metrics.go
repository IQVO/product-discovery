package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/IQVO/product-discovery/internal/application/ports"
)

// meterName scopes this service's own instruments, keeping them distinct from
// the ones otelchi and the runtime collector register.
const meterName = "github.com/IQVO/product-discovery"

// searchesServedName is the Tier 2 business metric of the context (ADR 0005, the
// fleet's standard metrics convention): every product search, by outcome.
const searchesServedName = "discovery.searches.served"

const (
	outcomeKey = attribute.Key("outcome")
	reasonKey  = attribute.Key("reason")

	outcomeServed = "served"
	outcomeEmpty  = "empty"
	outcomeFailed = "failed"
)

// SearchMetrics implements ports.SearchMetrics against the global
// MeterProvider. Until Setup installs a real provider the global one is a
// no-op, so recording is cheap and safe in tests and local runs.
type SearchMetrics struct {
	counter metric.Int64Counter
}

var _ ports.SearchMetrics = (*SearchMetrics)(nil)

// NewSearchMetrics registers the counter. It only fails if the instrument name
// is invalid, which is a programming error, not a runtime condition. Call it
// after Setup so the instrument binds to the real MeterProvider.
func NewSearchMetrics() (*SearchMetrics, error) {
	counter, err := otel.Meter(meterName).Int64Counter(
		searchesServedName,
		metric.WithDescription("Product searches by outcome (served with hits, empty for a first page that found nothing, "+
			"or failed; reason says why a failure happened: validation-failed or internal-error). "+
			"A rising empty share means shoppers look for products the storefront does not show, so check the projection "+
			"feeds (a stalled consumer leaves ProductView behind) and the catalogue; a rising failed with internal-error "+
			"means the search itself is failing, while validation-failed points at a client sending bad queries."),
		metric.WithUnit("{search}"),
	)
	if err != nil {
		return nil, err
	}
	return &SearchMetrics{counter: counter}, nil
}

// SearchServed counts a search that answered with hits.
func (m *SearchMetrics) SearchServed(ctx context.Context) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomeServed)))
}

// SearchEmpty counts a first page that found no product.
func (m *SearchMetrics) SearchEmpty(ctx context.Context) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomeEmpty)))
}

// SearchFailed counts a search that answered with an error, with the reason why.
func (m *SearchMetrics) SearchFailed(ctx context.Context, reason string) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomeFailed), reasonKey.String(reason)))
}
