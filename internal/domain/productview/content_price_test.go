package productview_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func TestContentRevisionGuard(t *testing.T) {
	m := must(t)
	pub := productview.PublishedContent{
		Revision: 2, Description: "d", Bullets: []string{"b"}, CategoryID: "c1",
		Attributes: []productview.Attribute{{Name: "n", Value: "v", Unit: "u"}},
		Media:      []productview.Media{{MediaID: "m", URL: "http://x", AltText: "a", Role: "PRIMARY"}},
	}
	v := m(stub(t).ApplyContentPublished(pub))
	if v.Content != productview.ContentPublished || v.ContentRevision != 2 || v.Description != "d" || v.CategoryID != "c1" ||
		!reflect.DeepEqual(v.Bullets, pub.Bullets) || !reflect.DeepEqual(v.Attributes, pub.Attributes) || !reflect.DeepEqual(v.Media, pub.Media) {
		t.Fatalf("content = %+v", v)
	}
	for _, rev := range []int64{1, 2} {
		old := pub
		old.Revision, old.Description = rev, "old"
		if got, out, _ := v.ApplyContentPublished(old); out != productview.Stale || got.Description != "d" {
			t.Errorf("revision %d must be stale", rev)
		}
	}
	old := pub
	old.Revision, old.Bullets = 3, nil
	if got, out, _ := v.ApplyContentPublished(old); out != productview.Applied || len(got.Bullets) != 0 || got.ContentRevision != 3 {
		t.Fatal("revision 3 replaces the content wholesale")
	}
}

func TestContentRejectsBadFacts(t *testing.T) {
	for _, f := range []productview.PublishedContent{{Revision: 0}, {Revision: -1}, {Revision: 1, CategoryID: "bad id"}} {
		if _, _, err := stub(t).ApplyContentPublished(f); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("%+v: %v", f, err)
		}
	}
	if _, _, err := stub(t).ApplyContentRetired(-1); !errors.Is(err, shared.ErrInvalidFact) {
		t.Errorf("negative retire revision: %v", err)
	}
}

func TestContentRetireGuardAndRepublish(t *testing.T) {
	m := must(t)
	v := m(stub(t).ApplyContentPublished(productview.PublishedContent{Revision: 3}))
	if _, out, _ := v.ApplyContentRetired(2); out != productview.Stale {
		t.Fatal("retiring an older revision is stale")
	}
	r, out, _ := v.ApplyContentRetired(3)
	if out != productview.Applied || r.Content != productview.ContentRetired || r.ContentRevision != 3 {
		t.Fatalf("retire = %+v", r)
	}
	if _, out, _ := r.ApplyContentRetired(3); out != productview.Stale {
		t.Fatal("retiring twice is stale")
	}
	if got, out, _ := v.ApplyContentRetired(0); out != productview.Applied || got.ContentRevision != 3 {
		t.Fatal("retire without a revision always applies and keeps the revision")
	}
	if got, _, _ := v.ApplyContentRetired(5); got.ContentRevision != 5 {
		t.Fatal("retire of a newer revision advances the guard")
	}
	later, out, _ := r.ApplyContentRetired(4)
	if out != productview.Applied || later.ContentRevision != 4 {
		t.Fatal("a newer retire advances the guard even when already retired")
	}
	if _, out, _ := later.ApplyContentPublished(productview.PublishedContent{Revision: 3}); out != productview.Stale {
		t.Fatal("a replayed older publish must not revive retired content")
	}
	again, out, _ := r.ApplyContentPublished(productview.PublishedContent{Revision: 4})
	if out != productview.Applied || again.Content != productview.ContentPublished {
		t.Fatal("a newer publish revives retired content")
	}
}

func TestPriceVersionGuard(t *testing.T) {
	m := must(t)
	p := productview.PriceChanged{Currency: "EUR", ListPriceMinor: 900, EffectivePriceMinor: 700, Version: 4}
	v := m(stub(t).ApplyPriceChanged(p))
	if v.Price != productview.PriceActive || v.PriceVersion != 4 || v.Currency != "EUR" || v.ListPriceMinor != 900 || v.EffectivePriceMinor != 700 {
		t.Fatalf("price = %+v", v)
	}
	for _, ver := range []int64{3, 4} {
		o := p
		o.Version, o.EffectivePriceMinor = ver, 1
		if got, out, _ := v.ApplyPriceChanged(o); out != productview.Stale || got.EffectivePriceMinor != 700 {
			t.Errorf("version %d must be stale", ver)
		}
	}
	p.Version, p.EffectivePriceMinor = 5, 600
	if got, out, _ := v.ApplyPriceChanged(p); out != productview.Applied || got.EffectivePriceMinor != 600 {
		t.Fatal("version 5 applies")
	}
}

func TestPriceRejectsBadFacts(t *testing.T) {
	ok := productview.PriceChanged{Currency: "USD", Version: 1}
	bad := []productview.PriceChanged{
		{Currency: "USD", Version: 0}, {Currency: "usd", Version: 1}, {Currency: "US", Version: 1}, {Currency: "USDD", Version: 1},
		{Currency: "USD", Version: 1, ListPriceMinor: -1}, {Currency: "USD", Version: 1, EffectivePriceMinor: -1},
	}
	if _, _, err := stub(t).ApplyPriceChanged(ok); err != nil {
		t.Fatal(err)
	}
	for _, f := range bad {
		if _, _, err := stub(t).ApplyPriceChanged(f); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("%+v: %v", f, err)
		}
	}
	if _, _, err := stub(t).ApplyPriceWithdrawn(0); !errors.Is(err, shared.ErrInvalidFact) {
		t.Errorf("withdrawn version 0: %v", err)
	}
}

func TestPriceWithdrawGuardAndReprice(t *testing.T) {
	m := must(t)
	v := m(stub(t).ApplyPriceChanged(productview.PriceChanged{Currency: "USD", ListPriceMinor: 9, EffectivePriceMinor: 8, Version: 4}))
	if _, out, _ := v.ApplyPriceWithdrawn(3); out != productview.Stale {
		t.Fatal("an older withdrawal is stale")
	}
	w, out, _ := v.ApplyPriceWithdrawn(4)
	if out != productview.Applied || w.Price != productview.PriceWithdrawn || w.HasPrice() || w.EffectivePriceMinor != 0 || w.ListPriceMinor != 0 {
		t.Fatalf("withdrawn = %+v", w)
	}
	if _, out, _ := w.ApplyPriceWithdrawn(4); out != productview.Stale {
		t.Fatal("withdrawing twice at the same version is stale")
	}
	newer, out, _ := w.ApplyPriceWithdrawn(5)
	if out != productview.Applied || newer.PriceVersion != 5 {
		t.Fatal("a newer withdrawal advances the guard even when already withdrawn")
	}
	if _, out, _ := newer.ApplyPriceChanged(productview.PriceChanged{Currency: "USD", Version: 5}); out != productview.Stale {
		t.Fatal("a replayed price below the withdrawal must not revive the row")
	}
	back, out, _ := w.ApplyPriceChanged(productview.PriceChanged{Currency: "USD", Version: 5, EffectivePriceMinor: 2})
	if out != productview.Applied || back.Price != productview.PriceActive {
		t.Fatal("a newer price revives a withdrawn one")
	}
	if _, out, _ := w.ApplyPriceChanged(productview.PriceChanged{Currency: "USD", Version: 4}); out != productview.Stale {
		t.Fatal("a price at the withdrawal version is stale")
	}
}

func TestSnapshotRestoreRoundTripAndIsolation(t *testing.T) {
	v := purchasable(t)
	got, err := productview.Restore(v.Snapshot())
	if err != nil || !reflect.DeepEqual(got, v) {
		t.Fatalf("round trip = %+v %v", got, err)
	}
	s := v.Snapshot()
	s.Listings[0].AvailableQty = 99
	if v.Listings[0].AvailableQty == 99 {
		t.Fatal("Snapshot must deep-copy listings")
	}
}

func TestRestoreRefusesCorruptRows(t *testing.T) {
	good := purchasable(t).Snapshot()
	mut := map[string]func(*productview.View){
		"id":      func(v *productview.View) { v.CatalogItemID = "" },
		"listing": func(v *productview.View) { v.Listings[0].ListingID = "bad id" },
		"qty":     func(v *productview.View) { v.Listings[0].AvailableQty = -1 },
		"state":   func(v *productview.View) { v.Listings[0].State = "GONE" },
	}
	for name, f := range mut {
		c := good.Snapshot()
		f(&c)
		if _, err := productview.Restore(c); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
