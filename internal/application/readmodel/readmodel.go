// Package readmodel holds the plain data that crosses the ports of
// product-discovery: the search request and result, and the rows of the
// snapshot reads the bootstrap rebuild folds. The ports package stays
// interfaces-only, so these structs live here.
package readmodel

import (
	"time"

	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// SearchRequest is what the SearchPort executes. The use case has already
// resolved the category tree: CategoryIDs is the subtree filter (nil = no
// filter, empty non-nil = matches nothing) and BoostCategoryIDs the categories
// whose name matches the text (they match and rank A).
type SearchRequest struct {
	Query            search.Query
	Mode             search.Mode
	CategoryIDs      []string
	BoostCategoryIDs []string
	Bands            []search.Band
}

// Hit is one matching row, without the category path (joined by the use case).
type Hit struct {
	CatalogItemID string
	Title         string
	CategoryID    string
	HasPrice      bool
	Currency      string
	PriceMinor    int64
	PrimaryMedia  *productview.Media
	InStock       bool
	Purchasable   bool
}

// SearchResult is one page: the hits, the facets over the whole filtered set,
// and the cursor of the next page (nil on the last).
type SearchResult struct {
	Hits   []Hit
	Facets []search.Facet
	Next   *search.Cursor
}

// Page is one page of a paged snapshot read; Next is empty on the last page.
type Page[T any] struct {
	Items []T
	Next  string
}

// CatalogRow is one item of GET /catalog.
type CatalogRow struct {
	CatalogItemID string
	Title         string
	Hazmat        bool
	Fragile       bool
	Discontinued  bool
}

// ListingRow is one listing of GET /listings.
type ListingRow struct {
	ListingID     string
	CatalogItemID string
	NodeID        string
	Suppressed    bool
	AvailableQty  int64
	AsOf          time.Time // zero before the first feed
}

// PriceRow is one item price of GET /item-prices.
type PriceRow struct {
	CatalogItemID       string
	Withdrawn           bool
	Currency            string
	ListPriceMinor      int64
	EffectivePriceMinor int64
	Version             int64
}

// ContentRow is one product content of GET /product-contents. Only PUBLISHED
// and RETIRED contents are folded; a DRAFT never was published.
type ContentRow struct {
	CatalogItemID string
	State         string // DRAFT, PUBLISHED or RETIRED
	Revision      int64
	Published     productview.PublishedContent
}

// CategoryTree is the answer of GET /categories.
type CategoryTree struct {
	Version    int64
	Categories []categoryview.Category
}
