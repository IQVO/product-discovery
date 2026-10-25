package repocontract

import (
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// spec describes one product fixture.
type spec struct {
	id, title, desc, category string
	price                     int64 // effective price in minor units; 0 = no price
	qty                       int64
	attrs                     []productview.Attribute
	withdrawn, discontinued   bool
	noContent                 bool
}

// build folds a spec through the domain fold rules, like the consumers do.
func build(t *testing.T, s spec) productview.View {
	t.Helper()
	v, err := productview.Stub(s.id)
	must(t, err)
	step := func(nv productview.View, _ productview.Outcome, err error) {
		t.Helper()
		must(t, err)
		v = nv
	}
	step(v.ApplyItemCreated(productview.ItemCreated{Title: s.title}))
	if s.discontinued {
		nv, _ := v.ApplyItemDiscontinued()
		v = nv
	}
	if !s.noContent {
		step(v.ApplyContentPublished(productview.PublishedContent{
			Revision: 1, Description: s.desc, CategoryID: s.category, Attributes: s.attrs,
			Media: []productview.Media{{MediaID: "m-" + s.id, URL: "https://cdn.example.test/" + s.id + ".jpg", AltText: "a", Role: "PRIMARY"}},
		}))
	}
	if s.price > 0 {
		step(v.ApplyPriceChanged(productview.PriceChanged{Currency: "USD", ListPriceMinor: s.price + 100, EffectivePriceMinor: s.price, Version: 1}))
	}
	if s.withdrawn {
		step(v.ApplyPriceWithdrawn(2))
	}
	if s.qty > 0 {
		step(v.ApplyAvailability(productview.AvailabilityChanged{ListingID: "L-" + s.id, NodeID: "N1", AvailableQty: s.qty, AsOf: t0}))
	}
	return v
}

func saveAll(t *testing.T, e Env, specs ...spec) {
	t.Helper()
	for _, s := range specs {
		must(t, e.Products.Save(bg(), build(t, s)))
	}
}

func tree(t *testing.T) categoryview.View {
	t.Helper()
	v, err := categoryview.New(4, []categoryview.Category{
		{ID: "home", Name: "Home", Path: "Home"},
		{ID: "kitchen", ParentID: "home", Name: "Kitchen", Path: "Home/Kitchen"},
		{ID: "kettles", ParentID: "kitchen", Name: "Kettles", Path: "Home/Kitchen/Kettles"},
		{ID: "garden", ParentID: "home", Name: "Garden", Path: "Home/Garden"},
	})
	must(t, err)
	return v
}

// categoryviewReplace returns a newer one-category tree.
func categoryviewReplace(t *testing.T, cur categoryview.View) (categoryview.View, bool, error) {
	t.Helper()
	next, applied, err := categoryview.Replace(cur, cur.Version()+1, []categoryview.Category{{ID: "solo", Name: "Solo", Path: "Solo"}})
	must(t, err)
	return next, applied, err
}
