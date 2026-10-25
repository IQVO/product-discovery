package kafka

import (
	"context"
	"log/slog"
	"time"

	ce "github.com/cloudevents/sdk-go/v2/event"

	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// Full CloudEvents types of the offer-availability events (network contracts).
const (
	TypeCatalogItemCreated         = "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated"
	TypeCatalogItemDiscontinued    = "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued"
	TypeListingCreated             = "com.retailnetwork.commerce.offer-availability.listing.ListingCreated"
	TypeListingAvailabilityChanged = "com.retailnetwork.commerce.offer-availability.listing.ListingAvailabilityChanged"
	TypeListingSuppressed          = "com.retailnetwork.commerce.offer-availability.listing.ListingSuppressed"
	TypeListingRestored            = "com.retailnetwork.commerce.offer-availability.listing.ListingRestored"
)

type catalogItemData struct {
	CatalogItemID string `json:"catalogItemId"`
	Title         string `json:"title"`
	Hazmat        bool   `json:"hazmat"`
	Fragile       bool   `json:"fragile"`
}

type listingData struct {
	ListingID     string    `json:"listingId"`
	CatalogItemID string    `json:"catalogItemId"`
	NodeID        string    `json:"nodeId"`
	AvailableQty  int64     `json:"availableQty"`
	AsOf          time.Time `json:"asOf"`
}

// NewOfferAvailabilityConsumer builds the consumer of offer-availability's
// event topic under groupID.
func NewOfferAvailabilityConsumer(brokers []string, groupID string, c usecases.Consumer, logger *slog.Logger) *Consumer {
	return newConsumer("offer-availability", brokers, OfferAvailabilityTopic, groupID, offerAvailabilityHandlers(c), logger)
}

func offerAvailabilityHandlers(c usecases.Consumer) map[string]HandlerFunc {
	return map[string]HandlerFunc{
		TypeCatalogItemCreated: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[catalogItemData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyCatalogItemCreated(ctx, meta(e), usecases.CatalogItemCreated{
				CatalogItemID: d.CatalogItemID, Title: d.Title, Hazmat: d.Hazmat, Fragile: d.Fragile,
			}))
		},
		TypeCatalogItemDiscontinued: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[catalogItemData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyCatalogItemDiscontinued(ctx, meta(e), usecases.CatalogItemDiscontinued{CatalogItemID: d.CatalogItemID}))
		},
		TypeListingCreated: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[listingData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyListingCreated(ctx, meta(e), usecases.ListingCreated{ListingID: d.ListingID, CatalogItemID: d.CatalogItemID, NodeID: d.NodeID}))
		},
		TypeListingAvailabilityChanged: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[listingData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyListingAvailabilityChanged(ctx, meta(e), usecases.ListingAvailabilityChanged{
				ListingID: d.ListingID, CatalogItemID: d.CatalogItemID, NodeID: d.NodeID, AvailableQty: d.AvailableQty, AsOf: d.AsOf,
			}))
		},
		TypeListingSuppressed: listingState(c, true),
		TypeListingRestored:   listingState(c, false),
	}
}

func listingState(c usecases.Consumer, suppressed bool) HandlerFunc {
	return func(ctx context.Context, e ce.Event) error {
		d, err := decodeData[listingData](e)
		if err != nil {
			return err
		}
		return classify(c.ApplyListingStateChanged(ctx, meta(e), usecases.ListingStateChanged{ListingID: d.ListingID, Suppressed: suppressed}))
	}
}
