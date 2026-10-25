package bdd

import (
	"context"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var bg = context.Background

func (w *world) publishTree(version int64, kettlesName string) error {
	return w.consumer.ApplyCategoryTreePublished(bg(), w.meta(), usecases.CategoryTreePublished{Version: version, Categories: []categoryview.Category{
		{ID: "home", Name: "Home", Path: "Home"},
		{ID: "kettles", ParentID: "home", Name: kettlesName, Path: "Home/" + kettlesName},
	}})
}

func (w *world) create(id, title string) error {
	return w.consumer.ApplyCatalogItemCreated(bg(), w.meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title})
}

func (w *world) stock(listing, item string, units int64, at time.Time) error {
	return w.consumer.ApplyListingAvailabilityChanged(bg(), w.meta(), usecases.ListingAvailabilityChanged{
		ListingID: listing, CatalogItemID: item, NodeID: "N1", AvailableQty: units, AsOf: at,
	})
}

func (w *world) content(id string, revision int64, category string) error {
	return w.consumer.ApplyProductContentPublished(bg(), w.meta(), usecases.ProductContentPublished{
		CatalogItemID: id, Content: productview.PublishedContent{Revision: revision, Description: "Described " + id, CategoryID: category},
	})
}

func (w *world) price(id string, effective, list int64) error {
	return w.consumer.ApplyItemPriceChanged(bg(), w.meta(), usecases.ItemPriceChanged{
		CatalogItemID: id, Price: productview.PriceChanged{Currency: "USD", ListPriceMinor: list, EffectivePriceMinor: effective, Version: 1},
	})
}

func (w *world) purchasable(id, title string, price int64, category string) error {
	for _, step := range []func() error{
		func() error { return w.create(id, title) },
		func() error { return w.stock("L-"+id, id, 5, t0) },
		func() error { return w.content(id, 1, category) },
		func() error { return w.price(id, price, price) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func registerFacts(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the category tree "Home > Kettles" is published$`, func() error { return w.publishTree(5, "Kettles") })
	sc.Step(`^the category tree is republished as version (\d+) with "kettles" renamed "([^"]*)"$`, func(v int64, name string) error {
		return w.publishTree(v, name)
	})
	sc.Step(`^the item master creates "([^"]*)" titled "([^"]*)"$`, w.create)
	sc.Step(`^the item master creates "([^"]*)" titled "([^"]*)" twice with the same event id$`, func(id, title string) error {
		m := w.meta()
		for _, t := range []string{title, "Changed"} {
			if err := w.consumer.ApplyCatalogItemCreated(bg(), m, usecases.CatalogItemCreated{CatalogItemID: id, Title: t}); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the item master discontinues "([^"]*)"$`, func(id string) error {
		return w.consumer.ApplyCatalogItemDiscontinued(bg(), w.meta(), usecases.CatalogItemDiscontinued{CatalogItemID: id})
	})
	sc.Step(`^offer-availability lists "([^"]*)" for "([^"]*)" with (\d+) units$`, func(listing, item string, units int64) error {
		return w.stock(listing, item, units, t0)
	})
	sc.Step(`^offer-availability reports (\d+) units for "([^"]*)" as of an older instant$`, func(units int64, listing string) error {
		return w.stock(listing, strings.TrimPrefix(listing, "L-"), units, t0.Add(-time.Hour))
	})
	sc.Step(`^offer-availability (suppresses|restores) listing "([^"]*)"$`, func(verb, listing string) error {
		return w.consumer.ApplyListingStateChanged(bg(), w.meta(), usecases.ListingStateChanged{ListingID: listing, Suppressed: verb == "suppresses"})
	})
	sc.Step(`^product-content publishes revision (\d+) for "([^"]*)" in category "([^"]*)"$`, func(rev int64, id, category string) error {
		return w.content(id, rev, category)
	})
	sc.Step(`^pricing sets the price of "([^"]*)" to (\d+) with list price (\d+)$`, w.price)
	sc.Step(`^a purchasable product "([^"]*)" titled "([^"]*)"$`, func(id, title string) error {
		return w.purchasable(id, title, 2500, "kettles")
	})
	sc.Step(`^a purchasable product "([^"]*)" titled "([^"]*)" priced (\d+) in category "([^"]*)"$`, w.purchasable)
}
