// Package productview holds ProductView, the projection one product page or
// search hit renders from, and the fold rules that build it from the facts of
// offer-availability, product-content and pricing. There is no aggregate: a
// row is a value that facts are folded into, guarded per source (listing asOf,
// content revision, price version) so replays and out-of-order delivery are
// safe, and recomputed into inStock and purchasable. It is pure: no I/O, no
// clock.
package productview

import (
	"fmt"
	"sort"
	"time"

	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// ItemState is the item master state. The empty state is a STUB: a row created
// by a fact that arrived before CatalogItemCreated (rule ErrNone).
type ItemState string

// Item states.
const (
	ItemStub         ItemState = ""
	ItemActive       ItemState = "ACTIVE"
	ItemDiscontinued ItemState = "DISCONTINUED"
)

// ContentState is the product-content state the row knows of.
type ContentState string

// Content states ("" = no content seen yet).
const (
	ContentNone      ContentState = ""
	ContentPublished ContentState = "PUBLISHED"
	ContentRetired   ContentState = "RETIRED"
)

// PriceState is the pricing state the row knows of.
type PriceState string

// Price states ("" = no price seen yet).
const (
	PriceNone      PriceState = ""
	PriceActive    PriceState = "ACTIVE"
	PriceWithdrawn PriceState = "WITHDRAWN"
)

// ListingState is the offer-availability listing state.
type ListingState string

// Listing states.
const (
	ListingLive       ListingState = "LIVE"
	ListingSuppressed ListingState = "SUPPRESSED"
)

// Attribute is one content attribute.
type Attribute struct {
	Name  string
	Value string
	Unit  string
}

// Media is one content medium.
type Media struct {
	MediaID string
	URL     string
	AltText string
	Role    string
}

// Listing is the availability of the item at one node.
type Listing struct {
	ListingID    string
	NodeID       string
	State        ListingState
	AvailableQty int64
	AsOf         time.Time // zero until the first availability fact
}

// View is the ProductView row of one catalogue item. The category is kept as an
// id only; its path is joined from CategoryView at query time (decision H3).
type View struct {
	CatalogItemID string

	Title   string
	Hazmat  bool
	Fragile bool
	Item    ItemState

	Content         ContentState
	ContentRevision int64
	Description     string
	Bullets         []string
	Attributes      []Attribute
	Media           []Media
	CategoryID      string

	Price               PriceState
	PriceVersion        int64
	Currency            string
	ListPriceMinor      int64
	EffectivePriceMinor int64

	Listings []Listing // ordered by ListingID
}

// Stub returns the invisible row an out-of-order fact creates.
func Stub(catalogItemID string) (View, error) {
	if !shared.ValidID(catalogItemID) {
		return View{}, fmt.Errorf("%w: catalogItemId %q", shared.ErrInvalidFact, catalogItemID)
	}
	return View{CatalogItemID: catalogItemID}, nil
}

// IsStub reports whether CatalogItemCreated has not completed the row yet; a
// stub is invisible to every read.
func (v View) IsStub() bool { return v.Title == "" }

// InStock: at least one LIVE listing with availableQty > 0.
func (v View) InStock() bool {
	for _, l := range v.Listings {
		if l.State == ListingLive && l.AvailableQty > 0 {
			return true
		}
	}
	return false
}

// Purchasable is the PurchasableRule: item ACTIVE, content PUBLISHED, price
// ACTIVE, and in stock.
func (v View) Purchasable() bool {
	return v.Item == ItemActive && v.Content == ContentPublished && v.Price == PriceActive && v.InStock()
}

// Searchable reports whether the row is offered in search and browse: a
// catalogued, active item with published content. (An unpurchasable one is
// still listed, as currently unavailable.)
func (v View) Searchable() bool { return v.Item == ItemActive && v.Content == ContentPublished }

// HasPrice reports whether the effective price may be shown.
func (v View) HasPrice() bool { return v.Price == PriceActive }

// Snapshot returns a deep copy of the row, for storage round trips.
func (v View) Snapshot() View {
	c := v
	c.Bullets = append([]string(nil), v.Bullets...)
	c.Attributes = append([]Attribute(nil), v.Attributes...)
	c.Media = append([]Media(nil), v.Media...)
	c.Listings = append([]Listing(nil), v.Listings...)
	sort.Slice(c.Listings, func(i, j int) bool { return c.Listings[i].ListingID < c.Listings[j].ListingID })
	return c
}

// Restore rebuilds a row from stored state, refusing one that could not have
// been produced by the fold rules.
func Restore(s View) (View, error) {
	if _, err := Stub(s.CatalogItemID); err != nil {
		return View{}, err
	}
	for _, l := range s.Listings {
		if !shared.ValidID(l.ListingID) || l.AvailableQty < 0 || (l.State != ListingLive && l.State != ListingSuppressed) {
			return View{}, fmt.Errorf("%w: stored listing %q", shared.ErrInvalidFact, l.ListingID)
		}
	}
	return s.Snapshot(), nil
}
