package bdd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// steps_doc_rebuild_test.go — the rebuild vocabulary of the documentation
// derived scenarios: the snapshot store seeded row by row, sources that serve
// a few rows per page or answer with an outage, and a run that reports its
// outcome instead of failing the step.

// pagedSources is the four rebuild source ports over the snapshot store of
// the world: it serves w.doc.pageSize rows per page (all when 0), counts the
// pages asked per source and answers an outage for the sources named down.
type pagedSources struct{ w *world }

func pageOf[T any](w *world, name, after string, rows []T) (readmodel.Page[T], error) {
	if w.doc.pages == nil {
		w.doc.pages = map[string]int{}
	}
	w.doc.pages[name]++
	if w.doc.down[name] {
		return readmodel.Page[T]{}, fmt.Errorf("the %s source answered 503", name)
	}
	start, _ := strconv.Atoi(after)
	end := len(rows)
	if size := w.doc.pageSize; size > 0 && start+size < end {
		end = start + size
	}
	page := readmodel.Page[T]{Items: rows[start:end]}
	if end < len(rows) {
		page.Next = strconv.Itoa(end)
	}
	return page, nil
}

func (p pagedSources) ListCatalog(_ context.Context, after string, _ int) (readmodel.Page[readmodel.CatalogRow], error) {
	return pageOf(p.w, "catalog", after, p.w.snap.catalog)
}

func (p pagedSources) ListListings(_ context.Context, after string, _ int) (readmodel.Page[readmodel.ListingRow], error) {
	return pageOf(p.w, "listings", after, p.w.snap.listings)
}

func (p pagedSources) ListItemPrices(_ context.Context, after string, _ int) (readmodel.Page[readmodel.PriceRow], error) {
	return pageOf(p.w, "item prices", after, p.w.snap.prices)
}

func (p pagedSources) ListProductContents(_ context.Context, after string, _ int) (readmodel.Page[readmodel.ContentRow], error) {
	return pageOf(p.w, "product contents", after, p.w.snap.contents)
}

func (p pagedSources) ListCategories(_ context.Context) (readmodel.CategoryTree, error) {
	if p.w.doc.down["categories"] {
		return readmodel.CategoryTree{}, fmt.Errorf("the categories source answered 503")
	}
	return p.w.snap.categories, nil
}

func registerDocRebuild(sc *godog.ScenarioContext, w *world) {
	registerDocSnapshotRows(sc, w)
	registerDocRebuildRun(sc, w)
}

func registerDocSnapshotRows(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the snapshot store lists the item "([^"]*)" titled "([^"]*)"( as discontinued)?$`, func(id, title, gone string) error {
		w.snap.catalog = append(w.snap.catalog, readmodel.CatalogRow{CatalogItemID: id, Title: title, Discontinued: gone != ""})
		return nil
	})
	sc.Step(`^the snapshot store lists the (suppressed |live )?listing "([^"]*)" of "([^"]*)" with (\d+) units at minute (\d+)$`,
		func(state, listing, item string, units, minute int64) error {
			w.snap.listings = append(w.snap.listings, readmodel.ListingRow{
				ListingID: listing, CatalogItemID: item, NodeID: "N1", Suppressed: state == "suppressed ", AvailableQty: units, AsOf: at(minute),
			})
			return nil
		})
	sc.Step(`^the snapshot store lists the price of "([^"]*)" at version (-?\d+) with effective (\d+) and list (\d+)$`,
		func(id string, version, effective, list int64) error {
			w.snap.prices = append(w.snap.prices, readmodel.PriceRow{
				CatalogItemID: id, Currency: "USD", ListPriceMinor: list, EffectivePriceMinor: effective, Version: version,
			})
			return nil
		})
	sc.Step(`^the snapshot store lists the withdrawn price of "([^"]*)" at version (\d+)$`, func(id string, version int64) error {
		w.snap.prices = append(w.snap.prices, readmodel.PriceRow{CatalogItemID: id, Withdrawn: true, Version: version})
		return nil
	})
	sc.Step(`^the snapshot store lists the (PUBLISHED|DRAFT|RETIRED) content of "([^"]*)" at revision (\d+) in category "([^"]*)"$`,
		func(state, id string, rev int64, category string) error {
			w.snap.contents = append(w.snap.contents, readmodel.ContentRow{
				CatalogItemID: id, State: state, Revision: rev,
				Published: productview.PublishedContent{Revision: rev, Description: "Described " + id, CategoryID: category},
			})
			return nil
		})
	sc.Step(`^the snapshot store holds the category tree at version (-?\d+) with:$`, func(version int64, t *godog.Table) error {
		w.treeSnapshot(version, treeRows(t)...)
		return nil
	})
}

func registerDocRebuildRun(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the snapshot source "([^"]*)" is down$`, func(name string) error {
		if w.doc.down == nil {
			w.doc.down = map[string]bool{}
		}
		w.doc.down[name] = true
		return nil
	})
	sc.Step(`^the snapshot source "([^"]*)" is back$`, func(name string) error {
		delete(w.doc.down, name)
		return nil
	})
	sc.Step(`^the snapshot sources serve (\d+) rows per page$`, func(n int) error {
		w.doc.pageSize = n
		return nil
	})
	sc.Step(`^the operator runs the rebuild$`, func() error {
		r := usecases.Rebuilder{Consumer: w.consumer, Sources: usecases.Sources{
			Catalog: pagedSources{w}, Listings: pagedSources{w}, Prices: pagedSources{w}, Contents: pagedSources{w},
		}}
		// the categories source is read through the contents port
		w.rebuild, w.rebuildErr = r.Run(bg())
		return nil
	})
	sc.Step(`^the rebuild (succeeds|stops at the step "([^"]*)")$`, func(outcome, step string) error {
		switch {
		case step == "" && w.rebuildErr != nil:
			return fmt.Errorf("the rebuild failed: %w", w.rebuildErr)
		case step != "" && (w.rebuildErr == nil || !strings.HasPrefix(w.rebuildErr.Error(), "rebuild "+step)):
			return fmt.Errorf("rebuild error = %v, want one at the step %q", w.rebuildErr, step)
		}
		return nil
	})
	sc.Step(`^the rebuild skipped (\d+) rows?$`, func(n int) error {
		if w.rebuild.Skipped != n {
			return fmt.Errorf("skipped = %d, want %d", w.rebuild.Skipped, n)
		}
		return nil
	})
	sc.Step(`^the "([^"]*)" source was read in (\d+) pages?$`, func(name string, n int) error {
		if got := w.doc.pages[name]; got != n {
			return fmt.Errorf("%s source read in %d pages, want %d", name, got, n)
		}
		return nil
	})
}
