package bdd

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// steps_wave2_test.go — the wave-2 vocabulary: the removal facts (content
// retired, price withdrawn), listing creation, and the bootstrap rebuild
// over hand-rolled snapshot sources.

// snapshot is the wave-2 fake of the four rebuild source ports: static rows
// served as one page (PageSize is 500, far above every fixture).
type snapshot struct {
	catalog    []readmodel.CatalogRow
	listings   []readmodel.ListingRow
	prices     []readmodel.PriceRow
	contents   []readmodel.ContentRow
	categories readmodel.CategoryTree
}

func (s *snapshot) ListCatalog(_ context.Context, _ string, _ int) (readmodel.Page[readmodel.CatalogRow], error) {
	return readmodel.Page[readmodel.CatalogRow]{Items: s.catalog}, nil
}

func (s *snapshot) ListListings(_ context.Context, _ string, _ int) (readmodel.Page[readmodel.ListingRow], error) {
	return readmodel.Page[readmodel.ListingRow]{Items: s.listings}, nil
}

func (s *snapshot) ListItemPrices(_ context.Context, _ string, _ int) (readmodel.Page[readmodel.PriceRow], error) {
	return readmodel.Page[readmodel.PriceRow]{Items: s.prices}, nil
}

func (s *snapshot) ListProductContents(_ context.Context, _ string, _ int) (readmodel.Page[readmodel.ContentRow], error) {
	return readmodel.Page[readmodel.ContentRow]{Items: s.contents}, nil
}

func (s *snapshot) ListCategories(_ context.Context) (readmodel.CategoryTree, error) {
	return s.categories, nil
}

func registerWave2(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^product-content retires "([^"]*)"$`, func(id string) error {
		return w.consumer.ApplyProductContentRetired(bg(), w.meta(), usecases.ProductContentRetired{CatalogItemID: id, Revision: 2})
	})
	sc.Step(`^pricing withdraws the price of "([^"]*)"$`, func(id string) error {
		return w.consumer.ApplyItemPriceWithdrawn(bg(), w.meta(), usecases.ItemPriceWithdrawn{CatalogItemID: id, Version: 2})
	})
	sc.Step(`^offer-availability creates listing "([^"]*)" for "([^"]*)" at node "([^"]*)"$`,
		func(listing, item, node string) error {
			return w.consumer.ApplyListingCreated(bg(), w.meta(), usecases.ListingCreated{ListingID: listing, CatalogItemID: item, NodeID: node})
		})

	sc.Step(`^the snapshot store holds one purchasable product "([^"]*)" titled "([^"]*)" priced (\d+) in category "([^"]*)"$`,
		func(id, title string, price int64, category string) error {
			w.stockSnapshots(id, title, price, category)
			return nil
		})
	sc.Step(`^the snapshot store holds the category tree "Home > Kettles"$`, func() error {
		w.treeSnapshot(5, categoryview.Category{ID: "home", Name: "Home", Path: "Home"},
			categoryview.Category{ID: "kettles", ParentID: "home", Name: "Kettles", Path: "Home/Kettles"})
		return nil
	})

	sc.Step(`^the projection rebuild runs over the upstream snapshots$`, func() error {
		r := usecases.Rebuilder{Consumer: w.consumer, Sources: usecases.Sources{
			Catalog: w.snap, Listings: w.snap, Prices: w.snap, Contents: w.snap,
		}}
		rep, err := r.Run(bg())
		w.rebuild, w.rebuildErr = rep, err
		return err
	})
	sc.Step(`^the rebuild folded (\d+) items?, (\d+) listings?, (\d+) prices?, (\d+) contents? and (\d+) categor(?:y|ies)$`,
		func(items, listings, prices, contents, categories int) error {
			r := w.rebuild
			if r.Items != items || r.Listings != listings || r.Prices != prices || r.Contents != contents || r.Categories != categories {
				return fmt.Errorf("rebuild report = %+v, want %d/%d/%d/%d/%d", r, items, listings, prices, contents, categories)
			}
			return nil
		})

	sc.Step(`^the shopper asks for the page after "([^"]*)"$`, func(cursor string) error {
		return w.get("/products?q=kettle&cursor=" + cursor)
	})
	sc.Step(`^the platform endpoint "([^"]*)" is requested$`, func(path string) error {
		return w.get(path)
	})
	sc.Step(`^the answer status is (\d+)$`, func(want int) error {
		if w.status != want {
			return fmt.Errorf("status = %d, want %d: %s", w.status, want, w.body)
		}
		return nil
	})
}

// stockSnapshots seeds the wave-2 snapshot fake with one fully described
// purchasable product (the same shape the "a purchasable product" step
// folds through events).
func (w *world) stockSnapshots(id, title string, price int64, category string) {
	w.snap.catalog = append(w.snap.catalog, readmodel.CatalogRow{CatalogItemID: id, Title: title})
	w.snap.listings = append(w.snap.listings, readmodel.ListingRow{
		ListingID: "L-" + id, CatalogItemID: id, NodeID: "N1", AvailableQty: 5, AsOf: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC),
	})
	w.snap.prices = append(w.snap.prices, readmodel.PriceRow{
		CatalogItemID: id, Currency: "USD", ListPriceMinor: price, EffectivePriceMinor: price, Version: 1,
	})
	w.snap.contents = append(w.snap.contents, readmodel.ContentRow{
		CatalogItemID: id, State: "PUBLISHED", Revision: 1,
		Published: productview.PublishedContent{Revision: 1, Description: "Described " + id, CategoryID: category},
	})
}

// treeSnapshot seeds the category tree of the snapshot fake.
func (w *world) treeSnapshot(version int64, cats ...categoryview.Category) {
	w.snap.categories = readmodel.CategoryTree{Version: version, Categories: cats}
}
