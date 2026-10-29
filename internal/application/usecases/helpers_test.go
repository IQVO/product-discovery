package usecases_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var (
	bg = context.Background
	t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
)

type env struct {
	store    *memory.Store
	deps     usecases.Deps
	consumer usecases.Consumer
	queries  usecases.Queries
	n        int
}

func newEnv() *env {
	s := memory.NewStore()
	d := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	return &env{store: s, deps: d, consumer: usecases.Consumer{Deps: d}, queries: usecases.Queries{Deps: d}}
}

// meta mints a fresh CloudEvents id.
func (e *env) meta() usecases.Meta {
	e.n++
	return usecases.Meta{EventID: fmt.Sprintf("ev-%d", e.n), Type: "test.Event", At: t0}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func (e *env) product(t *testing.T, id string) productview.View {
	t.Helper()
	v, err := e.deps.Products.Get(bg(), id)
	must(t, err)
	return v
}

func (e *env) notFound(t *testing.T, id string) {
	t.Helper()
	if _, err := e.queries.GetProduct(bg(), id); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetProduct(%s) = %v, want not found", id, err)
	}
}

// makePurchasable feeds the six events that make item id purchasable.
func (e *env) makePurchasable(t *testing.T, id, title, category string) {
	t.Helper()
	c := e.consumer
	must(t, c.ApplyCatalogItemCreated(bg(), e.meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title}))
	must(t, c.ApplyListingCreated(bg(), e.meta(), usecases.ListingCreated{ListingID: "L-" + id, CatalogItemID: id, NodeID: "N1"}))
	must(t, c.ApplyListingAvailabilityChanged(bg(), e.meta(), usecases.ListingAvailabilityChanged{
		ListingID: "L-" + id, CatalogItemID: id, NodeID: "N1", AvailableQty: 3, AsOf: t0,
	}))
	must(t, c.ApplyProductContentPublished(bg(), e.meta(), usecases.ProductContentPublished{
		CatalogItemID: id, Content: productview.PublishedContent{Revision: 1, CategoryID: category, Description: "d"},
	}))
	must(t, c.ApplyItemPriceChanged(bg(), e.meta(), usecases.ItemPriceChanged{
		CatalogItemID: id, Price: productview.PriceChanged{Currency: "USD", ListPriceMinor: 3000, EffectivePriceMinor: 2500, Version: 1},
	}))
}

func (e *env) publishTree(t *testing.T, version int64) {
	t.Helper()
	must(t, e.consumer.ApplyCategoryTreePublished(bg(), e.meta(), usecases.CategoryTreePublished{
		Version: version, Categories: []categoryview.Category{
			{ID: "home", Name: "Home", Path: "Home"},
			{ID: "kettles", ParentID: "home", Name: "Kettles", Path: "Home/Kettles"},
		},
	}))
}
