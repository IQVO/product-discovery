package usecases

import (
	"context"
	"errors"
	"time"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// CatalogItemCreated is the consumed offer-availability event of the same name.
type CatalogItemCreated struct {
	CatalogItemID string
	Title         string
	Hazmat        bool
	Fragile       bool
}

// CatalogItemDiscontinued is the consumed offer-availability event of the same
// name.
type CatalogItemDiscontinued struct{ CatalogItemID string }

// ListingCreated is the consumed offer-availability event of the same name.
type ListingCreated struct{ ListingID, CatalogItemID, NodeID string }

// ListingAvailabilityChanged is the consumed offer-availability event of the
// same name.
type ListingAvailabilityChanged struct {
	ListingID, CatalogItemID, NodeID string
	AvailableQty                     int64
	AsOf                             time.Time
}

// ListingStateChanged is ListingSuppressed (Suppressed true) or
// ListingRestored (false); both carry only the listing id.
type ListingStateChanged struct {
	ListingID  string
	Suppressed bool
}

// ApplyCatalogItemCreated completes the row of the item (rule ErrNone: a row
// the earlier facts stubbed becomes visible now).
func (c Consumer) ApplyCatalogItemCreated(ctx context.Context, m Meta, in CatalogItemCreated) error {
	return c.fold(ctx, m, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
		return v.ApplyItemCreated(productview.ItemCreated{Title: in.Title, Hazmat: in.Hazmat, Fragile: in.Fragile})
	})
}

// ApplyCatalogItemDiscontinued marks the item DISCONTINUED.
func (c Consumer) ApplyCatalogItemDiscontinued(ctx context.Context, m Meta, in CatalogItemDiscontinued) error {
	return c.fold(ctx, m, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
		next, out := v.ApplyItemDiscontinued()
		return next, out, nil
	})
}

// ApplyListingCreated adds the listing to its item and teaches ListingItemMap.
func (c Consumer) ApplyListingCreated(ctx context.Context, m Meta, in ListingCreated) error {
	return c.handle(ctx, m, func(ctx context.Context) error {
		if err := c.learn(ctx, in.ListingID, in.CatalogItemID); err != nil {
			return err
		}
		return c.apply(ctx, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
			return v.ApplyListingCreated(productview.ListingCreated{ListingID: in.ListingID, NodeID: in.NodeID})
		})
	})
}

// ApplyListingAvailabilityChanged sets the listing quantity under the asOf
// guard. The event carries the item id, so it also teaches ListingItemMap.
func (c Consumer) ApplyListingAvailabilityChanged(ctx context.Context, m Meta, in ListingAvailabilityChanged) error {
	return c.handle(ctx, m, func(ctx context.Context) error {
		if err := c.learn(ctx, in.ListingID, in.CatalogItemID); err != nil {
			return err
		}
		return c.apply(ctx, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
			return v.ApplyAvailability(productview.AvailabilityChanged{
				ListingID: in.ListingID, NodeID: in.NodeID, AvailableQty: in.AvailableQty, AsOf: in.AsOf,
			})
		})
	})
}

// ApplyListingStateChanged suppresses or restores a listing through
// ListingItemMap. A listing the map does not know is skipped, not retried
// (ADR 0002 A1).
func (c Consumer) ApplyListingStateChanged(ctx context.Context, m Meta, in ListingStateChanged) error {
	if !shared.ValidID(in.ListingID) {
		return invalidFact("listing id %q", in.ListingID)
	}
	return c.handle(ctx, m, func(ctx context.Context) error {
		itemID, err := c.Listings.Get(ctx, in.ListingID)
		if errors.Is(err, ports.ErrNotFound) {
			c.logger().Warn("listing state fact skipped: listing unknown", "listingId", in.ListingID, "eventId", m.EventID)
			return nil
		}
		if err != nil {
			return err
		}
		state := productview.ListingLive
		if in.Suppressed {
			state = productview.ListingSuppressed
		}
		return c.apply(ctx, itemID, func(v productview.View) (productview.View, productview.Outcome, error) {
			next, out := v.ApplyListingState(in.ListingID, state)
			return next, out, nil
		})
	})
}

// learn records listingId -> catalogItemId, refusing ids the projection cannot
// hold (a poison message).
func (c Consumer) learn(ctx context.Context, listingID, itemID string) error {
	if !shared.ValidID(listingID) || !shared.ValidID(itemID) {
		return invalidFact("listing %q of item %q", listingID, itemID)
	}
	return c.Listings.Put(ctx, listingID, itemID)
}
