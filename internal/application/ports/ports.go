// Package ports declares the application's OUT ports: interfaces only
// (enforced by internal/architecture). Their non-interface vocabulary is the
// sentinel of errors.go and the data package readmodel.
//
// Inside a UnitOfWork every repository joins the transaction carried by the ctx
// the unit handed to its function. Projections are written by up to three
// consumer groups at once, so the read-modify-write of a row goes through the
// GetForUpdate methods, which serialise writers per row.
package ports

import (
	"context"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// ProductRepository persists the ProductView rows.
type ProductRepository interface {
	// Get returns the row, or ErrNotFound. A stub row is returned as is.
	Get(ctx context.Context, catalogItemID string) (productview.View, error)
	// GetForUpdate is Get inside a unit of work, serialising other writers of
	// the same item until the unit ends. A missing row is ErrNotFound.
	GetForUpdate(ctx context.Context, catalogItemID string) (productview.View, error)
	// Save upserts the row, including its search document.
	Save(ctx context.Context, v productview.View) error
	// PurchasableByCategory counts the purchasable rows per category id.
	PurchasableByCategory(ctx context.Context) (map[string]int, error)
}

// CategoryRepository persists the one CategoryView.
type CategoryRepository interface {
	// Current returns the stored view; before the first publication it is
	// the zero view (version 0), not an error.
	Current(ctx context.Context) (categoryview.View, error)
	// CurrentForUpdate is Current inside a unit of work, serialising writers.
	CurrentForUpdate(ctx context.Context) (categoryview.View, error)
	// Replace stores the view wholesale.
	Replace(ctx context.Context, v categoryview.View) error
}

// ListingItemMap is the read-side lookup listingId to catalogItemId (H1).
type ListingItemMap interface {
	// Get returns the item of the listing, or ErrNotFound.
	Get(ctx context.Context, listingID string) (string, error)
	// Put records the mapping; a listing keeps its first item.
	Put(ctx context.Context, listingID, catalogItemID string) error
}

// Inbox makes consumers idempotent on the CloudEvents id.
type Inbox interface {
	// Claim records the event id and reports whether this is the first time
	// it is seen. Called with the ctx of a unit of work, the claim rolls back
	// with it.
	Claim(ctx context.Context, eventID, eventType string) (first bool, err error)
}

// UnitOfWork runs fn as ONE atomic unit: every repository and inbox call fn
// makes with the ctx it is handed commits together when fn returns nil, or
// rolls back together when fn returns an error (or panics). Do is re-entrant;
// the error fn returns is returned unchanged.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// SearchPort runs one search page against the projection (PostgreSQL
// full-text search plus pg_trgm today; a dedicated engine is a later adapter).
type SearchPort interface {
	Search(ctx context.Context, req readmodel.SearchRequest) (readmodel.SearchResult, error)
}

// CatalogSource reads offer-availability GET /catalog (bootstrap only).
type CatalogSource interface {
	ListCatalog(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.CatalogRow], error)
}

// ListingSource reads offer-availability GET /listings (bootstrap only, the
// ops-only in-org read).
type ListingSource interface {
	ListListings(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.ListingRow], error)
}

// PriceSource reads pricing GET /item-prices (bootstrap only).
type PriceSource interface {
	ListItemPrices(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.PriceRow], error)
}

// ContentSource reads product-content GET /product-contents and GET
// /categories (bootstrap only).
type ContentSource interface {
	ListProductContents(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.ContentRow], error)
	ListCategories(ctx context.Context) (readmodel.CategoryTree, error)
}

// SearchMetrics records the business outcome of every SearchProducts call (Tier 2
// of the fleet metrics convention). The use case calls exactly one method per
// call: SearchServed when the page has hits (or is a continuation page),
// SearchEmpty when a first page found nothing even after the trigram retry,
// SearchFailed otherwise, with a stable low-cardinality reason. The
// implementation lives in internal/adapters/outbound/telemetry; a nil
// Deps.Metrics records nothing.
type SearchMetrics interface {
	// SearchServed counts a search that answered with hits.
	SearchServed(ctx context.Context)
	// SearchEmpty counts a first page that found no product.
	SearchEmpty(ctx context.Context)
	// SearchFailed counts a search that answered with an error; reason is
	// "validation-failed" or "internal-error".
	SearchFailed(ctx context.Context, reason string)
}
