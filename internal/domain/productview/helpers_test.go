package productview_test

import (
	"testing"
	"time"

	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func stub(t *testing.T) productview.View {
	t.Helper()
	v, err := productview.Stub("CI-1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func must(t *testing.T) func(productview.View, productview.Outcome, error) productview.View {
	return func(v productview.View, _ productview.Outcome, err error) productview.View {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// purchasable returns a view that satisfies every clause of PurchasableRule.
func purchasable(t *testing.T) productview.View {
	t.Helper()
	m := must(t)
	v := m(stub(t).ApplyItemCreated(productview.ItemCreated{Title: "Kettle"}))
	v = m(v.ApplyListingCreated(productview.ListingCreated{ListingID: "L1", NodeID: "N1"}))
	v = m(v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N1", AvailableQty: 3, AsOf: t0}))
	v = m(v.ApplyContentPublished(productview.PublishedContent{Revision: 1, CategoryID: "kettles"}))
	return m(v.ApplyPriceChanged(productview.PriceChanged{Currency: "USD", ListPriceMinor: 3000, EffectivePriceMinor: 2500, Version: 1}))
}

func secs(n int64) time.Duration { return time.Duration(n) * time.Second }
