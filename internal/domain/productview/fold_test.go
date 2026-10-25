package productview_test

import (
	"errors"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func TestStubRejectsBadIDAndIsInvisible(t *testing.T) {
	if _, err := productview.Stub("bad id"); !errors.Is(err, shared.ErrInvalidFact) {
		t.Fatalf("err = %v", err)
	}
	if !stub(t).IsStub() {
		t.Fatal("a fresh row is a stub")
	}
}

func TestItemCreatedCompletesStub(t *testing.T) {
	m := must(t)
	v, out, err := stub(t).ApplyItemCreated(productview.ItemCreated{Title: " Kettle ", Hazmat: true, Fragile: true})
	if err != nil || out != productview.Applied || v.IsStub() || v.Title != "Kettle" || !v.Hazmat || !v.Fragile || v.Item != productview.ItemActive {
		t.Fatalf("created = %+v %v %v", v, out, err)
	}
	again, out, _ := v.ApplyItemCreated(productview.ItemCreated{Title: "Other"})
	if out != productview.Stale || again.Title != "Kettle" {
		t.Fatal("a second create must not change the title")
	}
	_ = m
	for _, title := range []string{"", "   ", string(make([]rune, productview.MaxTitle+1))} {
		if _, _, err := stub(t).ApplyItemCreated(productview.ItemCreated{Title: title}); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("title %.5q: err = %v", title, err)
		}
	}
}

func TestDiscontinueBeforeCreateStaysDiscontinued(t *testing.T) {
	v, out := stub(t).ApplyItemDiscontinued()
	if out != productview.Applied || !v.IsStub() {
		t.Fatalf("discontinued stub: %+v", v)
	}
	v, out, err := v.ApplyItemCreated(productview.ItemCreated{Title: "Kettle"})
	if err != nil || out != productview.Applied || v.Item != productview.ItemDiscontinued || v.IsStub() {
		t.Fatalf("late create = %+v %v", v, err)
	}
	if _, out := v.ApplyItemDiscontinued(); out != productview.Stale {
		t.Fatal("discontinuing twice is a no-op")
	}
}

func TestAvailabilityGuardIsStrictlyMonotonic(t *testing.T) {
	m := must(t)
	v := m(stub(t).ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 5, AsOf: t0}))
	if len(v.Listings) != 1 || v.Listings[0].State != productview.ListingLive {
		t.Fatalf("an availability for an unknown listing adds it LIVE: %+v", v.Listings)
	}
	for _, at := range []int64{0, -1} {
		old, out, _ := v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 99, AsOf: t0.Add(secs(at))})
		if out != productview.Stale || old.Listings[0].AvailableQty != 5 {
			t.Errorf("asOf delta %d must be stale", at)
		}
	}
	newer, out, _ := v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 1, AsOf: t0.Add(secs(1))})
	if out != productview.Applied || newer.Listings[0].AvailableQty != 1 {
		t.Fatal("a newer asOf applies")
	}
	if v.Listings[0].AvailableQty != 5 {
		t.Fatal("the fold must not mutate the receiver's listings")
	}
}

func TestAvailabilityAndListingRejectBadFacts(t *testing.T) {
	bad := []productview.AvailabilityChanged{
		{ListingID: "", NodeID: "N", AsOf: t0},
		{ListingID: "L", NodeID: "", AsOf: t0},
		{ListingID: "L", NodeID: "N", AvailableQty: -1, AsOf: t0},
		{ListingID: "L", NodeID: "N"},
	}
	for _, f := range bad {
		if _, _, err := stub(t).ApplyAvailability(f); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("%+v: err = %v", f, err)
		}
	}
	if _, _, err := stub(t).ApplyListingCreated(productview.ListingCreated{ListingID: "L"}); !errors.Is(err, shared.ErrInvalidFact) {
		t.Errorf("listing without node: %v", err)
	}
	if _, _, err := stub(t).ApplyListingCreated(productview.ListingCreated{NodeID: "N"}); !errors.Is(err, shared.ErrInvalidFact) {
		t.Errorf("listing without id: %v", err)
	}
}

func TestListingCreatedAfterAvailabilityKeepsQuantity(t *testing.T) {
	m := must(t)
	v := m(stub(t).ApplyAvailability(productview.AvailabilityChanged{ListingID: "L1", NodeID: "N", AvailableQty: 4, AsOf: t0}))
	v2, out, err := v.ApplyListingCreated(productview.ListingCreated{ListingID: "L1", NodeID: "N"})
	if err != nil || out != productview.Stale || v2.Listings[0].AvailableQty != 4 {
		t.Fatalf("late create = %+v %v %v", v2, out, err)
	}
	v3 := m(v.ApplyListingCreated(productview.ListingCreated{ListingID: "A0", NodeID: "N"}))
	if v3.Listings[0].ListingID != "A0" || len(v3.Listings) != 2 {
		t.Fatalf("listings must stay ordered by id: %+v", v3.Listings)
	}
}

func TestListingStateForUnknownListingUpserts(t *testing.T) {
	v, out := stub(t).ApplyListingState("L9", productview.ListingSuppressed)
	if out != productview.Applied || len(v.Listings) != 1 || v.Listings[0].State != productview.ListingSuppressed {
		t.Fatalf("upsert = %+v", v.Listings)
	}
}
