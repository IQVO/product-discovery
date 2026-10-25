package kafka

import (
	"context"
	"log/slog"

	ce "github.com/cloudevents/sdk-go/v2/event"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// Full CloudEvents types of the pricing events.
const (
	TypeItemPriceChanged   = "com.retailnetwork.storefront.pricing.itemprice.ItemPriceChanged"
	TypeItemPriceWithdrawn = "com.retailnetwork.storefront.pricing.itemprice.ItemPriceWithdrawn"
)

type priceData struct {
	CatalogItemID       string `json:"catalogItemId"`
	Currency            string `json:"currency"`
	ListPriceMinor      int64  `json:"listPriceMinor"`
	EffectivePriceMinor int64  `json:"effectivePriceMinor"`
	Version             int64  `json:"version"`
}

// NewPricingConsumer builds the consumer of pricing's event topic under groupID.
func NewPricingConsumer(brokers []string, groupID string, c usecases.Consumer, logger *slog.Logger) *Consumer {
	return newConsumer("pricing", brokers, PricingTopic, groupID, pricingHandlers(c), logger)
}

func pricingHandlers(c usecases.Consumer) map[string]HandlerFunc {
	return map[string]HandlerFunc{
		TypeItemPriceChanged: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[priceData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyItemPriceChanged(ctx, meta(e), usecases.ItemPriceChanged{
				CatalogItemID: d.CatalogItemID,
				Price: productview.PriceChanged{
					Currency: d.Currency, ListPriceMinor: d.ListPriceMinor, EffectivePriceMinor: d.EffectivePriceMinor, Version: d.Version,
				},
			}))
		},
		TypeItemPriceWithdrawn: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[priceData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyItemPriceWithdrawn(ctx, meta(e), usecases.ItemPriceWithdrawn{CatalogItemID: d.CatalogItemID, Version: d.Version}))
		},
	}
}
