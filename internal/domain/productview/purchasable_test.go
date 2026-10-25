package productview_test

import (
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/productview"
)

func TestPurchasableNeedsEveryClause(t *testing.T) {
	if !purchasable(t).Purchasable() {
		t.Fatal("the full view must be purchasable")
	}
	cases := map[string]func(v productview.View) productview.View{
		"discontinued": func(v productview.View) productview.View { v, _ = v.ApplyItemDiscontinued(); return v },
		"content retired": func(v productview.View) productview.View {
			v, _, _ = v.ApplyContentRetired(1)
			return v
		},
		"price withdrawn": func(v productview.View) productview.View {
			v, _, _ = v.ApplyPriceWithdrawn(2)
			return v
		},
		"listing suppressed": func(v productview.View) productview.View {
			v, _ = v.ApplyListingState("L1", productview.ListingSuppressed)
			return v
		},
		"sold out": func(v productview.View) productview.View {
			v, _, _ = v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N1", AvailableQty: 0, AsOf: t0.Add(1)})
			return v
		},
	}
	for name, mutate := range cases {
		if mutate(purchasable(t)).Purchasable() {
			t.Errorf("%s: must not be purchasable", name)
		}
	}
}

func TestMissingClausesBlockPurchasable(t *testing.T) {
	m := must(t)
	base := m(stub(t).ApplyItemCreated(productview.ItemCreated{Title: "Kettle"}))
	if base.Purchasable() || base.InStock() || base.Searchable() || base.HasPrice() {
		t.Fatal("a bare item is none of them")
	}
	noContent := m(base.ApplyPriceChanged(productview.PriceChanged{Currency: "USD", Version: 1}))
	noContent = m(noContent.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 1, AsOf: t0}))
	if noContent.Purchasable() || !noContent.InStock() || !noContent.HasPrice() {
		t.Fatal("without content the item is in stock and priced, never purchasable")
	}
	noPrice := m(base.ApplyContentPublished(productview.PublishedContent{Revision: 1}))
	noPrice = m(noPrice.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 1, AsOf: t0}))
	if noPrice.Purchasable() || !noPrice.Searchable() {
		t.Fatal("without a price the item is searchable but not purchasable")
	}
}

func TestInStockIgnoresSuppressedAndEmptyListings(t *testing.T) {
	m := must(t)
	v := stub(t)
	v = m(v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "A", NodeID: "N", AvailableQty: 5, AsOf: t0}))
	v = m(v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "B", NodeID: "N", AvailableQty: 0, AsOf: t0}))
	v, _ = v.ApplyListingState("A", productview.ListingSuppressed)
	if v.InStock() {
		t.Fatal("suppressed or empty listings never count")
	}
	v, out := v.ApplyListingState("A", productview.ListingLive)
	if out != productview.Applied || !v.InStock() {
		t.Fatal("a restored listing counts again")
	}
	if _, out := v.ApplyListingState("A", productview.ListingLive); out != productview.Stale {
		t.Fatal("restoring a LIVE listing is a no-op")
	}
}
