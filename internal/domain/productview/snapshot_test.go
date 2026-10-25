package productview_test

import (
	"errors"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func TestSnapshotListingTakesItsStateWhenNoStateFactSetOne(t *testing.T) {
	m := must(t)
	snap := productview.ListingSnapshot{ListingID: "L1", NodeID: "N", Suppressed: true, AvailableQty: 4, AsOf: t0}
	fresh := m(stub(t).ApplyListingSnapshot(snap))
	if l := fresh.Listings[0]; l.State != productview.ListingSuppressed || !l.StateKnown || l.AvailableQty != 4 || l.NodeID != "N" {
		t.Fatalf("new listing = %+v", l)
	}
	// An availability fact came first: the state is unknown, so the snapshot decides.
	viaStock := m(stub(t).ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N0", AvailableQty: 9, AsOf: t0.Add(-secs(5))}))
	got := m(viaStock.ApplyListingSnapshot(snap))
	if l := got.Listings[0]; l.State != productview.ListingSuppressed || l.NodeID != "N" || l.AvailableQty != 4 {
		t.Fatalf("listing created by a stock fact = %+v", l)
	}
}

func TestSnapshotNeverRewindsAKnownStateOrNewerStock(t *testing.T) {
	m := must(t)
	v := m(stub(t).ApplyListingCreated(productview.ListingCreated{ListingID: "L1", NodeID: "N"}))
	v = m(v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 7, AsOf: t0.Add(secs(10))}))
	got, out, err := v.ApplyListingSnapshot(productview.ListingSnapshot{ListingID: "L1", NodeID: "N", Suppressed: true, AvailableQty: 1, AsOf: t0})
	if err != nil || out != productview.Stale {
		t.Fatalf("an older snapshot of a known listing changes nothing: %v %v", out, err)
	}
	if l := got.Listings[0]; l.State != productview.ListingLive || l.AvailableQty != 7 {
		t.Fatalf("listing = %+v", l)
	}
	newer, out, _ := v.ApplyListingSnapshot(productview.ListingSnapshot{ListingID: "L1", NodeID: "N", Suppressed: true, AvailableQty: 2, AsOf: t0.Add(secs(20))})
	if l := newer.Listings[0]; out != productview.Applied || l.AvailableQty != 2 || l.State != productview.ListingLive {
		t.Fatalf("a newer snapshot moves the quantity only: %+v", l)
	}
}

func TestSnapshotListingWithoutAFeedAndBadFacts(t *testing.T) {
	m := must(t)
	v := m(stub(t).ApplyListingSnapshot(productview.ListingSnapshot{ListingID: "L1", NodeID: "N"}))
	if l := v.Listings[0]; !l.AsOf.IsZero() || l.AvailableQty != 0 || l.State != productview.ListingLive {
		t.Fatalf("a listing before its first feed = %+v", l)
	}
	for _, bad := range []productview.ListingSnapshot{{ListingID: "", NodeID: "N"}, {ListingID: "L", NodeID: ""}, {ListingID: "L", NodeID: "N", AvailableQty: -1, AsOf: t0}} {
		if _, _, err := stub(t).ApplyListingSnapshot(bad); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
