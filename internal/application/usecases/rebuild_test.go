package usecases_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// fakeSources serves slices in pages of two, keyed by the position as cursor.
type fakeSources struct {
	catalog  []readmodel.CatalogRow
	listings []readmodel.ListingRow
	prices   []readmodel.PriceRow
	contents []readmodel.ContentRow
	tree     readmodel.CategoryTree
	failAt   string
	pages    int
}

func slice[T any](rows []T, after string) readmodel.Page[T] {
	start, _ := strconv.Atoi(after)
	end := start + 2
	if end >= len(rows) {
		return readmodel.Page[T]{Items: rows[min(start, len(rows)):]}
	}
	return readmodel.Page[T]{Items: rows[start:end], Next: strconv.Itoa(end)}
}

var errSource = errors.New("source down")

func (f *fakeSources) ListCatalog(_ context.Context, after string, _ int) (readmodel.Page[readmodel.CatalogRow], error) {
	f.pages++
	if f.failAt == "catalog" {
		return readmodel.Page[readmodel.CatalogRow]{}, errSource
	}
	return slice(f.catalog, after), nil
}

func (f *fakeSources) ListListings(_ context.Context, after string, _ int) (readmodel.Page[readmodel.ListingRow], error) {
	return slice(f.listings, after), nil
}

func (f *fakeSources) ListItemPrices(_ context.Context, after string, _ int) (readmodel.Page[readmodel.PriceRow], error) {
	return slice(f.prices, after), nil
}

func (f *fakeSources) ListProductContents(_ context.Context, after string, _ int) (readmodel.Page[readmodel.ContentRow], error) {
	return slice(f.contents, after), nil
}

func (f *fakeSources) ListCategories(context.Context) (readmodel.CategoryTree, error) {
	return f.tree, nil
}

func (e *env) rebuilder(f *fakeSources) usecases.Rebuilder {
	return usecases.Rebuilder{Consumer: e.consumer, Sources: usecases.Sources{Catalog: f, Listings: f, Prices: f, Contents: f}}
}

func snapshot() *fakeSources {
	return &fakeSources{
		catalog: []readmodel.CatalogRow{
			{CatalogItemID: "A", Title: "Alpha"}, {CatalogItemID: "B", Title: "Beta"},
			{CatalogItemID: "C", Title: "Gamma", Discontinued: true},
		},
		listings: []readmodel.ListingRow{
			{ListingID: "LA", CatalogItemID: "A", NodeID: "N", AvailableQty: 4, AsOf: t0},
			{ListingID: "LB", CatalogItemID: "B", NodeID: "N", Suppressed: true},
		},
		prices: []readmodel.PriceRow{
			{CatalogItemID: "A", Currency: "USD", ListPriceMinor: 900, EffectivePriceMinor: 800, Version: 3},
			{CatalogItemID: "B", Withdrawn: true, Version: 5},
		},
		contents: []readmodel.ContentRow{
			{CatalogItemID: "A", State: "PUBLISHED", Revision: 2, Published: productview.PublishedContent{Revision: 2, CategoryID: "c1"}},
			{CatalogItemID: "B", State: "RETIRED", Revision: 4},
			{CatalogItemID: "C", State: "DRAFT", Revision: 1},
		},
		tree: readmodel.CategoryTree{Version: 7, Categories: []categoryview.Category{{ID: "c1", Name: "One", Path: "One"}}},
	}
}

func TestRebuildSeedsProjectionsAndGuards(t *testing.T) {
	e := newEnv()
	rep, err := e.rebuilder(snapshot()).Run(bg())
	must(t, err)
	if rep.Items != 3 || rep.Listings != 2 || rep.Prices != 2 || rep.Contents != 3 || rep.Categories != 1 {
		t.Fatalf("report = %+v", rep)
	}
	a := e.product(t, "A")
	if !a.Purchasable() || a.PriceVersion != 3 || a.ContentRevision != 2 || a.Listings[0].AsOf != t0 {
		t.Fatalf("A = %+v", a)
	}
	if b := e.product(t, "B"); b.Price != productview.PriceWithdrawn || b.Content != productview.ContentRetired || b.InStock() {
		t.Fatalf("B = %+v", b)
	}
	if c := e.product(t, "C"); c.Item != productview.ItemDiscontinued || c.Content != productview.ContentNone {
		t.Fatalf("C = %+v", c)
	}
}

func TestRebuildSeededGuardsDropReplayedEvents(t *testing.T) {
	e := newEnv()
	_, err := e.rebuilder(snapshot()).Run(bg())
	must(t, err)
	must(t, e.consumer.ApplyItemPriceChanged(bg(), e.meta(), usecases.ItemPriceChanged{
		CatalogItemID: "A", Price: productview.PriceChanged{Currency: "USD", EffectivePriceMinor: 1, Version: 3},
	}))
	if e.product(t, "A").EffectivePriceMinor != 800 {
		t.Fatal("the replayed price must be stale")
	}
	if tree, _ := e.queries.ListCategories(bg()); tree.Version != 7 {
		t.Fatalf("tree = %+v", tree)
	}
}

func TestRebuildIsIdempotentAndKeepsNewerEventState(t *testing.T) {
	e := newEnv()
	r := e.rebuilder(snapshot())
	_, err := r.Run(bg())
	must(t, err)
	// An event after the snapshot restores B's listing; a second rebuild must not undo it.
	must(t, e.consumer.ApplyListingStateChanged(bg(), e.meta(), usecases.ListingStateChanged{ListingID: "LB"}))
	_, err = r.Run(bg())
	must(t, err)
	if b := e.product(t, "B"); b.Listings[0].State != productview.ListingLive {
		t.Fatalf("a rebuild must not overwrite a listing's event-driven state: %+v", b.Listings)
	}
	if n := e.store.Counts().Products; n != 3 {
		t.Fatalf("products = %d", n)
	}
}

func TestRebuildReportsTheFailingSourceAndWalksEveryPage(t *testing.T) {
	e := newEnv()
	f := snapshot()
	f.failAt = "catalog"
	_, err := e.rebuilder(f).Run(bg())
	if !errors.Is(err, errSource) {
		t.Fatalf("err = %v", err)
	}
	f = snapshot()
	_, err = e.rebuilder(f).Run(bg())
	must(t, err)
	if f.pages != 2 {
		t.Fatalf("3 catalog rows in pages of 2 need 2 reads, got %d", f.pages)
	}
	ctx, cancel := context.WithCancel(bg())
	cancel()
	if _, err := newEnv().rebuilder(snapshot()).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled rebuild stops: %v", err)
	}
}

func TestRebuildRefusesPoisonRows(t *testing.T) {
	e := newEnv()
	f := snapshot()
	f.listings = []readmodel.ListingRow{{ListingID: "bad id", CatalogItemID: "A", NodeID: "N"}}
	if _, err := e.rebuilder(f).Run(bg()); err == nil {
		t.Fatal("an unfoldable row must abort the run")
	}
	f = snapshot()
	f.listings = []readmodel.ListingRow{{ListingID: "L1", CatalogItemID: "A", NodeID: "N", AvailableQty: -1, AsOf: t0}}
	if _, err := e.rebuilder(f).Run(bg()); err == nil {
		t.Fatal("a negative quantity must abort the run")
	}
}

func TestRebuildSkipsAnUnpublishedTree(t *testing.T) {
	e := newEnv()
	f := snapshot()
	f.tree = readmodel.CategoryTree{}
	rep, err := e.rebuilder(f).Run(bg())
	must(t, err)
	if rep.Categories != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got, _ := e.queries.ListCategories(bg()); got.Version != 0 {
		t.Fatalf("tree = %+v", got)
	}
}
