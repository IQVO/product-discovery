package usecases_test

import (
	"errors"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func TestElevenEventsMakeAnItemPurchasable(t *testing.T) {
	e := newEnv()
	e.publishTree(t, 1)
	e.makePurchasable(t, "I1", "Steel Kettle", "kettles")
	p, err := e.queries.GetProduct(bg(), "I1")
	must(t, err)
	if !p.View.Purchasable() || p.View.Title != "Steel Kettle" || len(p.CategoryPath) != 2 || p.CategoryPath[1] != "Kettles" {
		t.Fatalf("product = %+v", p)
	}
	c := e.consumer
	must(t, c.ApplyListingStateChanged(bg(), e.meta(), usecases.ListingStateChanged{ListingID: "L-I1", Suppressed: true}))
	if e.product(t, "I1").InStock() {
		t.Fatal("a suppressed listing must not count")
	}
	must(t, c.ApplyListingStateChanged(bg(), e.meta(), usecases.ListingStateChanged{ListingID: "L-I1"}))
	must(t, c.ApplyItemPriceWithdrawn(bg(), e.meta(), usecases.ItemPriceWithdrawn{CatalogItemID: "I1", Version: 2}))
	if e.product(t, "I1").Purchasable() {
		t.Fatal("a withdrawn price must block")
	}
	must(t, c.ApplyProductContentRetired(bg(), e.meta(), usecases.ProductContentRetired{CatalogItemID: "I1", Revision: 1}))
	must(t, c.ApplyCatalogItemDiscontinued(bg(), e.meta(), usecases.CatalogItemDiscontinued{CatalogItemID: "I1"}))
	if v := e.product(t, "I1"); v.Item != productview.ItemDiscontinued || v.Content != productview.ContentRetired {
		t.Fatalf("view = %+v", v)
	}
}

func TestFactsBeforeTheItemCreateStayInvisible(t *testing.T) {
	e := newEnv()
	c := e.consumer
	must(t, c.ApplyItemPriceChanged(bg(), e.meta(), usecases.ItemPriceChanged{
		CatalogItemID: "I2", Price: productview.PriceChanged{Currency: "USD", EffectivePriceMinor: 5, Version: 1},
	}))
	must(t, c.ApplyProductContentPublished(bg(), e.meta(), usecases.ProductContentPublished{
		CatalogItemID: "I2", Content: productview.PublishedContent{Revision: 1},
	}))
	e.notFound(t, "I2")
	stub := e.product(t, "I2")
	if !stub.IsStub() || stub.Price != productview.PriceActive || stub.Content != productview.ContentPublished {
		t.Fatalf("stub = %+v", stub)
	}
	must(t, c.ApplyCatalogItemCreated(bg(), e.meta(), usecases.CatalogItemCreated{CatalogItemID: "I2", Title: "Late"}))
	p, err := e.queries.GetProduct(bg(), "I2")
	must(t, err)
	if p.View.Title != "Late" || p.View.EffectivePriceMinor != 5 {
		t.Fatalf("completed = %+v", p.View)
	}
}

func TestDuplicateEventIDIsAppliedOnce(t *testing.T) {
	e := newEnv()
	m := e.meta()
	in := usecases.CatalogItemCreated{CatalogItemID: "I3", Title: "Once"}
	must(t, e.consumer.ApplyCatalogItemCreated(bg(), m, in))
	in.Title = "Twice"
	must(t, e.consumer.ApplyCatalogItemCreated(bg(), m, in))
	if got := e.product(t, "I3").Title; got != "Once" {
		t.Fatalf("title = %q: the redelivery must be acknowledged without effect", got)
	}
}

func TestStaleFactsAreAcknowledgedWithoutEffect(t *testing.T) {
	e := newEnv()
	e.makePurchasable(t, "I4", "Kettle", "kettles")
	c := e.consumer
	must(t, c.ApplyListingAvailabilityChanged(bg(), e.meta(), usecases.ListingAvailabilityChanged{
		ListingID: "L-I4", CatalogItemID: "I4", NodeID: "N1", AvailableQty: 0, AsOf: t0.Add(-1),
	}))
	must(t, c.ApplyItemPriceChanged(bg(), e.meta(), usecases.ItemPriceChanged{
		CatalogItemID: "I4", Price: productview.PriceChanged{Currency: "USD", EffectivePriceMinor: 1, Version: 1},
	}))
	must(t, c.ApplyProductContentPublished(bg(), e.meta(), usecases.ProductContentPublished{
		CatalogItemID: "I4", Content: productview.PublishedContent{Revision: 1, Description: "old"},
	}))
	v := e.product(t, "I4")
	if !v.InStock() || v.EffectivePriceMinor != 2500 || v.Description != "d" {
		t.Fatalf("stale facts changed the row: %+v", v)
	}
}

func TestListingStateForUnknownListingIsSkipped(t *testing.T) {
	e := newEnv()
	before := e.store.Counts()
	must(t, e.consumer.ApplyListingStateChanged(bg(), e.meta(), usecases.ListingStateChanged{ListingID: "L-ghost", Suppressed: true}))
	after := e.store.Counts()
	if after.Products != before.Products || after.Inbox != before.Inbox+1 {
		t.Fatalf("counts %+v -> %+v: skipped means acknowledged, nothing stored", before, after)
	}
	err := e.consumer.ApplyListingStateChanged(bg(), e.meta(), usecases.ListingStateChanged{ListingID: "bad id"})
	if !errors.Is(err, shared.ErrInvalidFact) {
		t.Fatalf("invalid listing id: %v", err)
	}
}

func TestPoisonFactsAreInvalidAndStoreNothing(t *testing.T) {
	e := newEnv()
	c := e.consumer
	errs := []error{
		c.ApplyCatalogItemCreated(bg(), e.meta(), usecases.CatalogItemCreated{CatalogItemID: "bad id", Title: "x"}),
		c.ApplyCatalogItemCreated(bg(), e.meta(), usecases.CatalogItemCreated{CatalogItemID: "ok", Title: ""}),
		c.ApplyListingCreated(bg(), e.meta(), usecases.ListingCreated{ListingID: "bad id", CatalogItemID: "ok", NodeID: "N"}),
		c.ApplyListingAvailabilityChanged(bg(), e.meta(), usecases.ListingAvailabilityChanged{ListingID: "L", CatalogItemID: "ok", NodeID: "N", AvailableQty: -1, AsOf: t0}),
		c.ApplyItemPriceChanged(bg(), e.meta(), usecases.ItemPriceChanged{CatalogItemID: "ok", Price: productview.PriceChanged{Currency: "usd", Version: 1}}),
		c.ApplyCategoryTreePublished(bg(), e.meta(), usecases.CategoryTreePublished{Version: 0}),
	}
	for i, err := range errs {
		if !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
	if n := e.store.Counts(); n.Products != 0 || n.Inbox != 0 || n.Listings != 0 {
		t.Fatalf("a rejected fact must roll back everything, counts %+v", n)
	}
}

func TestCategoryTreeVersionGuard(t *testing.T) {
	e := newEnv()
	e.publishTree(t, 3)
	must(t, e.consumer.ApplyCategoryTreePublished(bg(), e.meta(), usecases.CategoryTreePublished{Version: 2}))
	got, err := e.queries.ListCategories(bg())
	must(t, err)
	if got.Version != 3 || len(got.Categories) != 2 {
		t.Fatalf("an older tree must be ignored: %+v", got)
	}
}
