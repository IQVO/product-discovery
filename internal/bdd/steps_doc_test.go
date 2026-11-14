package bdd

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/cucumber/godog"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// steps_doc_test.go — the vocabulary of the documentation-derived scenarios
// (features/*_rules.feature): facts with explicit versions and instants,
// facts the projection may refuse, a content draft, and category trees given
// as tables. The read-side assertions live in steps_doc_read_test.go and the
// rebuild vocabulary in steps_doc_rebuild_test.go.

// docState is the per-scenario state of the documentation-derived steps.
type docState struct {
	// err is the answer of the last fact a scenario announced with the
	// "announces" steps (which do not fail the step when the fact is refused).
	err error
	// draft is the product content being composed by the "draft" steps.
	draft productview.PublishedContent
	// header is the header set of the last raw request.
	header http.Header
	// etag is the validator noted by "noting its ETag".
	etag string
	// down names the rebuild sources that currently answer with an outage.
	down map[string]bool
	// pageSize is how many rows the rebuild sources serve per page (0 = all).
	pageSize int
	// pages counts the pages each rebuild source was asked for.
	pages map[string]int
}

// at is the instant `minute` minutes after the first report of the suite.
func at(minute int64) time.Time { return t0.Add(time.Duration(minute) * time.Minute) }

// try records the outcome of a fact that may be refused.
func (w *world) try(err error) error {
	w.doc.err = err
	return nil
}

func registerDoc(sc *godog.ScenarioContext, w *world) {
	registerDocItems(sc, w)
	registerDocListings(sc, w)
	registerDocPrices(sc, w)
	registerDocContent(sc, w)
	registerDocTrees(sc, w)
	registerDocRedelivery(sc, w)
	registerDocRead(sc, w)
	registerDocHTTP(sc, w)
	registerDocRebuild(sc, w)
}

func registerDocItems(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the item master creates "([^"]*)" titled "([^"]*)" with hazmat (true|false) and fragile (true|false)$`,
		func(id, title, hazmat, fragile string) error {
			return w.consumer.ApplyCatalogItemCreated(bg(), w.meta(), usecases.CatalogItemCreated{
				CatalogItemID: id, Title: title, Hazmat: hazmat == "true", Fragile: fragile == "true",
			})
		})
	sc.Step(`^the item master announces "([^"]*)" titled with (\d+) characters$`, func(id string, n int) error {
		return w.try(w.consumer.ApplyCatalogItemCreated(bg(), w.meta(), usecases.CatalogItemCreated{
			CatalogItemID: id, Title: strings.Repeat("k", n),
		}))
	})
	sc.Step(`^the item master announces the item "([^"]*)" titled "([^"]*)"$`, func(id, title string) error {
		return w.try(w.consumer.ApplyCatalogItemCreated(bg(), w.meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title}))
	})
	sc.Step(`^the item master announces an item whose id is (\d+) characters long$`, func(n int) error {
		return w.try(w.consumer.ApplyCatalogItemCreated(bg(), w.meta(), usecases.CatalogItemCreated{
			CatalogItemID: strings.Repeat("i", n), Title: "Long Id Kettle",
		}))
	})
	sc.Step(`^the fact is (accepted|refused as invalid)$`, func(outcome string) error {
		err := w.doc.err
		switch {
		case outcome == "accepted" && err != nil:
			return fmt.Errorf("the fact was refused: %w", err)
		case outcome != "accepted" && !errors.Is(err, shared.ErrInvalidFact):
			return fmt.Errorf("the fact was not refused as invalid: %w", err)
		}
		return nil
	})
}

func registerDocListings(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^offer-availability reports (\d+) units for listing "([^"]*)" of "([^"]*)" at minute (-?\d+)$`,
		func(units int64, listing, item string, minute int64) error {
			return w.stock(listing, item, units, at(minute))
		})
	sc.Step(`^offer-availability announces listing "([^"]*)" of "([^"]*)" with (-?\d+) units$`,
		func(listing, item string, units int64) error {
			return w.try(w.stock(listing, item, units, t0))
		})
	sc.Step(`^offer-availability announces listing "([^"]*)" of "([^"]*)" with (\d+) units and no as-of instant$`,
		func(listing, item string, units int64) error {
			return w.try(w.stock(listing, item, units, time.Time{}))
		})
}

func registerDocPrices(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^pricing sets version (\d+) of the price of "([^"]*)" to (\d+) with list price (\d+)$`,
		func(version int64, id string, effective, list int64) error {
			return w.consumer.ApplyItemPriceChanged(bg(), w.meta(), usecases.ItemPriceChanged{
				CatalogItemID: id,
				Price:         productview.PriceChanged{Currency: "USD", ListPriceMinor: list, EffectivePriceMinor: effective, Version: version},
			})
		})
	sc.Step(`^pricing announces version (-?\d+) of the price of "([^"]*)" in currency "([^"]*)" at effective (-?\d+) and list (-?\d+)$`,
		func(version int64, id, currency string, effective, list int64) error {
			return w.try(w.consumer.ApplyItemPriceChanged(bg(), w.meta(), usecases.ItemPriceChanged{
				CatalogItemID: id,
				Price:         productview.PriceChanged{Currency: currency, ListPriceMinor: list, EffectivePriceMinor: effective, Version: version},
			}))
		})
	sc.Step(`^pricing withdraws the price of "([^"]*)" at version (\d+)$`, func(id string, version int64) error {
		return w.consumer.ApplyItemPriceWithdrawn(bg(), w.meta(), usecases.ItemPriceWithdrawn{CatalogItemID: id, Version: version})
	})
}

func registerDocContent(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a content draft described "([^"]*)"$`, func(text string) error {
		w.doc.draft = productview.PublishedContent{Description: text}
		return nil
	})
	sc.Step(`^the draft has the bullet "([^"]*)"$`, func(text string) error {
		w.doc.draft.Bullets = append(w.doc.draft.Bullets, text)
		return nil
	})
	sc.Step(`^the draft has the attribute "([^"]*)" = "([^"]*)"$`, func(name, value string) error {
		w.doc.draft.Attributes = append(w.doc.draft.Attributes, productview.Attribute{Name: name, Value: value})
		return nil
	})
	sc.Step(`^the draft has the attribute "([^"]*)" = "([^"]*)" in "([^"]*)"$`, func(name, value, unit string) error {
		w.doc.draft.Attributes = append(w.doc.draft.Attributes, productview.Attribute{Name: name, Value: value, Unit: unit})
		return nil
	})
	sc.Step(`^the draft has the (PRIMARY|GALLERY) image "([^"]*)"$`, func(role, id string) error {
		w.doc.draft.Media = append(w.doc.draft.Media, productview.Media{
			MediaID: id, URL: "https://img.example/" + id + ".jpg", AltText: id, Role: role,
		})
		return nil
	})
	sc.Step(`^product-content publishes the draft as revision (\d+) for "([^"]*)" in category "([^"]*)"$`,
		func(rev int64, id, category string) error { return w.publishDraft(id, rev, category) })
	sc.Step(`^product-content announces revision (-?\d+) for "([^"]*)" in category "([^"]*)"$`, func(rev int64, id, category string) error {
		return w.try(w.content(id, rev, category))
	})
	sc.Step(`^product-content retires "([^"]*)" at revision (\d+)$`, func(id string, rev int64) error {
		return w.consumer.ApplyProductContentRetired(bg(), w.meta(), usecases.ProductContentRetired{CatalogItemID: id, Revision: rev})
	})
}

func (w *world) publishDraft(id string, rev int64, category string) error {
	c := w.doc.draft
	c.Revision, c.CategoryID = rev, category
	return w.consumer.ApplyProductContentPublished(bg(), w.meta(), usecases.ProductContentPublished{CatalogItemID: id, Content: c})
}

// treeRows turns a headerless table of `id | parent | name` rows into the
// categories of a tree; the path of a category is its names joined by "/".
func treeRows(t *godog.Table) []categoryview.Category {
	parent, name := map[string]string{}, map[string]string{}
	var ids []string
	for _, row := range t.Rows {
		id := row.Cells[0].Value
		ids = append(ids, id)
		parent[id], name[id] = row.Cells[1].Value, row.Cells[2].Value
	}
	cats := make([]categoryview.Category, 0, len(ids))
	for _, id := range ids {
		path := name[id]
		for cur, hops := parent[id], 0; cur != "" && hops < 10; cur, hops = parent[cur], hops+1 {
			path = name[cur] + "/" + path
		}
		cats = append(cats, categoryview.Category{ID: id, ParentID: parent[id], Name: name[id], Path: path})
	}
	return cats
}

func (w *world) announceTree(version int64, t *godog.Table) error {
	return w.consumer.ApplyCategoryTreePublished(bg(), w.meta(), usecases.CategoryTreePublished{Version: version, Categories: treeRows(t)})
}

func registerDocTrees(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the category tree is published at version (\d+) with:$`, func(version int64, t *godog.Table) error {
		return w.announceTree(version, t)
	})
	sc.Step(`^the category tree is announced at version (\d+) with:$`, func(version int64, t *godog.Table) error {
		return w.try(w.announceTree(version, t))
	})
	sc.Step(`^the category tree is announced at version (\d+) with a (duplicate category id|parent that is not in the tree|parent cycle|category without a name)$`,
		func(version int64, defect string) error {
			cats := map[string][]categoryview.Category{
				"duplicate category id": {
					{ID: "home", Name: "Home", Path: "Home"}, {ID: "home", Name: "Again", Path: "Again"},
				},
				"parent that is not in the tree": {
					{ID: "home", Name: "Home", Path: "Home"}, {ID: "kettles", ParentID: "attic", Name: "Kettles", Path: "Home/Kettles"},
				},
				"parent cycle": {
					{ID: "a", ParentID: "b", Name: "A", Path: "A"}, {ID: "b", ParentID: "a", Name: "B", Path: "B"},
				},
				"category without a name": {
					{ID: "home", Name: "Home", Path: "Home"}, {ID: "kettles", ParentID: "home", Path: "Home/Kettles"},
				},
			}[defect]
			return w.try(w.consumer.ApplyCategoryTreePublished(bg(), w.meta(), usecases.CategoryTreePublished{Version: version, Categories: cats}))
		})
	sc.Step(`^the price bands are cut at (\d+) and (\d+)$`, func(a, b int64) error {
		w.srv.Close()
		w.deps.Bands = []int64{a, b}
		w.srv = httptest.NewServer(inboundhttp.NewRouter(&inboundhttp.Server{Queries: usecases.Queries{Deps: w.deps}}, nil, ""))
		return nil
	})
}

// registerDocRedelivery registers the "same event id again" steps: the inbox
// must drop the second delivery even though its payload would pass the guard.
func registerDocRedelivery(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^pricing delivers version (\d+) of the price of "([^"]*)" at (\d+) and then the same event again claiming version (\d+) at (\d+)$`,
		func(v1 int64, id string, p1, v2, p2 int64) error {
			m := w.meta()
			for _, p := range []struct{ version, price int64 }{{v1, p1}, {v2, p2}} {
				err := w.consumer.ApplyItemPriceChanged(bg(), m, usecases.ItemPriceChanged{
					CatalogItemID: id,
					Price:         productview.PriceChanged{Currency: "USD", ListPriceMinor: p.price, EffectivePriceMinor: p.price, Version: p.version},
				})
				if err != nil {
					return err
				}
			}
			return nil
		})
	sc.Step(`^offer-availability delivers (\d+) units for listing "([^"]*)" of "([^"]*)" at minute (\d+) and then the same event again with (\d+) units at minute (\d+)$`,
		func(u1 int64, listing, item string, m1 int64, u2 int64, m2 int64) error {
			m := w.meta()
			for _, d := range []struct{ units, minute int64 }{{u1, m1}, {u2, m2}} {
				err := w.consumer.ApplyListingAvailabilityChanged(bg(), m, usecases.ListingAvailabilityChanged{
					ListingID: listing, CatalogItemID: item, NodeID: "N1", AvailableQty: d.units, AsOf: at(d.minute),
				})
				if err != nil {
					return err
				}
			}
			return nil
		})
	sc.Step(`^the category tree is delivered at version (\d+) as "([^"]*)" and then the same event again at version (\d+) as "([^"]*)"$`,
		func(v1 int64, n1 string, v2 int64, n2 string) error {
			m := w.meta()
			for _, d := range []struct {
				version int64
				name    string
			}{{v1, n1}, {v2, n2}} {
				err := w.consumer.ApplyCategoryTreePublished(bg(), m, usecases.CategoryTreePublished{Version: d.version, Categories: []categoryview.Category{
					{ID: "home", Name: "Home", Path: "Home"},
					{ID: "kettles", ParentID: "home", Name: d.name, Path: "Home/" + d.name},
				}})
				if err != nil {
					return err
				}
			}
			return nil
		})
}
