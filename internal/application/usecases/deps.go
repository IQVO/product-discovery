// Package usecases holds the application's use cases: the three queries of
// apis/openapi.yaml, one fold per consumed event, and the bootstrap rebuild.
// Folds follow one shape inside ONE ports.UnitOfWork together with the inbox
// claim: load the row (or start a stub), call the domain fold rule, save when
// it applied. Domain and port errors pass through unchanged (errors.Is keeps
// working): the HTTP adapter maps them to problem types and the Kafka adapter
// decides between retry and dead letter.
package usecases

import (
	"log/slog"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// Deps bundles the ports the use cases need.
type Deps struct {
	Products   ports.ProductRepository
	Categories ports.CategoryRepository
	Listings   ports.ListingItemMap
	Inbox      ports.Inbox
	UoW        ports.UnitOfWork
	Search     ports.SearchPort
	// Bands are the price-band edges of the facet; nil means
	// search.DefaultBandEdges.
	Bands []int64
	// Log receives the facts a fold skipped on purpose; nil means slog.Default.
	Log *slog.Logger
}

func (d Deps) logger() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

func (d Deps) bands() []search.Band {
	if d.Bands == nil {
		return search.Bands(search.DefaultBandEdges)
	}
	return search.Bands(d.Bands)
}
