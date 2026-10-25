package repocontract

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

func testProducts(t *testing.T, e Env) {
	_, err := e.Products.Get(bg(), "nope")
	wantErr(t, err, ports.ErrNotFound)
	full := build(t, spec{
		id: "p1", title: "Steel Kettle", desc: "Boils", category: "kettles", price: 3000, qty: 4,
		attrs: []productview.Attribute{{Name: "capacity", Value: "1.7", Unit: "l"}},
	})
	full.Bullets = []string{"b1", "b2"}
	must(t, e.Products.Save(bg(), full))
	got, err := e.Products.Get(bg(), "p1")
	must(t, err)
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, full)
	}
	stub, _ := productview.Stub("p2")
	must(t, e.Products.Save(bg(), stub))
	if got, err := e.Products.Get(bg(), "p2"); err != nil || !got.IsStub() {
		t.Fatalf("a stub round trips: %+v %v", got, err)
	}
	full.Title = "Renamed"
	full.Listings = nil
	must(t, e.Products.Save(bg(), full))
	got, _ = e.Products.Get(bg(), "p1")
	if got.Title != "Renamed" || len(got.Listings) != 0 {
		t.Fatalf("Save must upsert wholesale: %+v", got)
	}
	_, err = e.Products.GetForUpdate(bg(), "ghost")
	wantErr(t, err, ports.ErrNotFound)
	must(t, e.UoW.Do(bg(), func(ctx context.Context) error {
		_, err := e.Products.GetForUpdate(ctx, "p1")
		return err
	}))
}

func testPurchasableCounts(t *testing.T, e Env) {
	saveAll(t, e,
		spec{id: "a", title: "A", category: "kettles", price: 100, qty: 1},
		spec{id: "b", title: "B", category: "kettles", price: 100, qty: 1},
		spec{id: "c", title: "C", category: "kettles", price: 100},        // out of stock
		spec{id: "d", title: "D", category: "garden", price: 100, qty: 2}, // purchasable
		spec{id: "e", title: "E", category: "garden", qty: 2},             // no price
		spec{id: "f", title: "F", price: 100, qty: 2},                     // no category
		spec{id: "g", title: "G", category: "garden", price: 100, qty: 1, discontinued: true},
	)
	got, err := e.Products.PurchasableByCategory(bg())
	must(t, err)
	if want := map[string]int{"kettles": 2, "garden": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PurchasableByCategory = %v, want %v", got, want)
	}
}

func testCategories(t *testing.T, e Env) {
	cur, err := e.Categories.Current(bg())
	must(t, err)
	if cur.Version() != 0 || len(cur.Categories()) != 0 {
		t.Fatalf("before the first publication the view is empty: %+v", cur)
	}
	want := tree(t)
	must(t, e.Categories.Replace(bg(), want))
	got, err := e.Categories.Current(bg())
	must(t, err)
	if got.Version() != 4 || !reflect.DeepEqual(got.Categories(), want.Categories()) {
		t.Fatalf("round trip: %+v", got.Categories())
	}
	next, _, _ := categoryviewReplace(t, got)
	must(t, e.Categories.Replace(bg(), next))
	again, _ := e.Categories.Current(bg())
	if again.Version() != 5 || len(again.Categories()) != 1 {
		t.Fatalf("Replace must be wholesale: %+v", again.Categories())
	}
	must(t, e.UoW.Do(bg(), func(ctx context.Context) error {
		_, err := e.Categories.CurrentForUpdate(ctx)
		return err
	}))
}

func testListings(t *testing.T, e Env) {
	_, err := e.Listings.Get(bg(), "L1")
	wantErr(t, err, ports.ErrNotFound)
	must(t, e.Listings.Put(bg(), "L1", "item-1"))
	must(t, e.Listings.Put(bg(), "L1", "item-2")) // a listing keeps its first item
	got, err := e.Listings.Get(bg(), "L1")
	must(t, err)
	if got != "item-1" {
		t.Fatalf("listing item = %q, want item-1", got)
	}
}

func testInbox(t *testing.T, e Env) {
	first, err := e.Inbox.Claim(bg(), "ev-1", "type.A")
	must(t, err)
	again, err := e.Inbox.Claim(bg(), "ev-1", "type.A")
	must(t, err)
	other, err := e.Inbox.Claim(bg(), "ev-2", "type.A")
	must(t, err)
	if !first || again || !other {
		t.Fatalf("claims = %v %v %v, want true false true", first, again, other)
	}
}

var errBoom = errors.New("boom")

func testUnitOfWork(t *testing.T, e Env) {
	t.Run("a unit that fails rolls back every table", func(t *testing.T) {
		err := e.UoW.Do(bg(), func(ctx context.Context) error {
			saveCtx(t, ctx, e, spec{id: "rb", title: "Rollback"})
			must(t, e.Categories.Replace(ctx, tree(t)))
			must(t, e.Listings.Put(ctx, "L-rb", "rb"))
			if first, err := e.Inbox.Claim(ctx, "ev-rb", "type.A"); err != nil || !first {
				t.Fatalf("the claim inside the unit must be the first: %v %v", first, err)
			}
			return errBoom
		})
		wantErr(t, err, errBoom)
		_, err = e.Products.Get(bg(), "rb")
		wantErr(t, err, ports.ErrNotFound)
		if cur, _ := e.Categories.Current(bg()); cur.Version() != 0 {
			t.Fatal("the tree must be rolled back")
		}
		_, err = e.Listings.Get(bg(), "L-rb")
		wantErr(t, err, ports.ErrNotFound)
		if first, _ := e.Inbox.Claim(bg(), "ev-rb", "type.A"); !first {
			t.Fatal("the rolled back claim must be free again")
		}
	})
	t.Run("a unit that succeeds commits, and a nested unit joins it", func(t *testing.T) {
		must(t, e.UoW.Do(bg(), func(ctx context.Context) error {
			return e.UoW.Do(ctx, func(ctx context.Context) error {
				saveCtx(t, ctx, e, spec{id: "ok", title: "Committed"})
				return nil
			})
		}))
		if _, err := e.Products.Get(bg(), "ok"); err != nil {
			t.Fatalf("committed row missing: %v", err)
		}
		err := e.UoW.Do(bg(), func(ctx context.Context) error {
			must(t, e.UoW.Do(ctx, func(ctx context.Context) error {
				saveCtx(t, ctx, e, spec{id: "nest", title: "Nested"})
				return nil
			}))
			return errBoom
		})
		wantErr(t, err, errBoom)
		_, err = e.Products.Get(bg(), "nest")
		wantErr(t, err, ports.ErrNotFound)
	})
}

func saveCtx(t *testing.T, ctx context.Context, e Env, s spec) {
	t.Helper()
	must(t, e.Products.Save(ctx, build(t, s)))
}
