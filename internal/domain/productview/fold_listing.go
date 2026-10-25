package productview

import (
	"sort"
	"time"

	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// ListingCreated is ListingCreated; it also teaches ListingItemMap.
type ListingCreated struct {
	ListingID string
	NodeID    string
}

// AvailabilityChanged is ListingAvailabilityChanged.
type AvailabilityChanged struct {
	ListingID    string
	NodeID       string
	AvailableQty int64
	AsOf         time.Time
}

func (v View) listingIndex(id string) int {
	for i, l := range v.Listings {
		if l.ListingID == id {
			return i
		}
	}
	return -1
}

// withListing returns v with listing l inserted or replaced, ordered by id,
// without touching the slice v shares with its caller.
func (v View) withListing(l Listing) View {
	out := make([]Listing, 0, len(v.Listings)+1)
	for _, x := range v.Listings {
		if x.ListingID != l.ListingID {
			out = append(out, x)
		}
	}
	out = append(out, l)
	sort.Slice(out, func(i, j int) bool { return out[i].ListingID < out[j].ListingID })
	v.Listings = out
	return v
}

// ApplyListingCreated adds a LIVE listing with no quantity. A listing the row
// already knows (its availability fact arrived first) is left as it is.
func (v View) ApplyListingCreated(f ListingCreated) (View, Outcome, error) {
	if !shared.ValidID(f.ListingID) || !shared.ValidID(f.NodeID) {
		return v, Stale, invalid("listing %q at node %q", f.ListingID, f.NodeID)
	}
	if v.listingIndex(f.ListingID) >= 0 {
		return v, Stale, nil
	}
	return v.withListing(Listing{ListingID: f.ListingID, NodeID: f.NodeID, State: ListingLive}), Applied, nil
}

// ApplyAvailability sets the quantity of a listing when asOf is strictly newer
// than the listing's last asOf (the per-listing monotonic guard). An unknown
// listing is added LIVE.
func (v View) ApplyAvailability(f AvailabilityChanged) (View, Outcome, error) {
	if !shared.ValidID(f.ListingID) || !shared.ValidID(f.NodeID) || f.AvailableQty < 0 || f.AsOf.IsZero() {
		return v, Stale, invalid("availability of listing %q: qty %d asOf %v", f.ListingID, f.AvailableQty, f.AsOf)
	}
	l := Listing{ListingID: f.ListingID, NodeID: f.NodeID, State: ListingLive}
	if i := v.listingIndex(f.ListingID); i >= 0 {
		l = v.Listings[i]
		if !f.AsOf.After(l.AsOf) {
			return v, Stale, nil
		}
	}
	l.AvailableQty, l.AsOf = f.AvailableQty, f.AsOf
	return v.withListing(l), Applied, nil
}

// ApplyListingState sets a listing SUPPRESSED or LIVE (ListingSuppressed,
// ListingRestored). A suppressed listing never counts towards inStock.
func (v View) ApplyListingState(listingID string, state ListingState) (View, Outcome) {
	i := v.listingIndex(listingID)
	if i >= 0 && v.Listings[i].State == state {
		return v, Stale
	}
	l := Listing{ListingID: listingID, State: state}
	if i >= 0 {
		l = v.Listings[i]
		l.State = state
	}
	return v.withListing(l), Applied
}
