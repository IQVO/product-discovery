package sources_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/sources"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

func serve(t *testing.T, routes map[string]string) (sources.Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := routes[r.URL.RequestURI()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return sources.Client{BaseURL: srv.URL + "/", Pause: time.Millisecond}, &hits
}

func same(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %+v\nwant %+v", what, got, want)
	}
}

func TestCatalogMapsTheNetworkContract(t *testing.T) {
	c, _ := serve(t, map[string]string{
		"/catalog?after=c1&limit=2": `{"items":[{"catalogItemId":"I1","title":"Kettle","hazmat":true,"fragile":false,"state":"ACTIVE","lengthMm":5},
			{"catalogItemId":"I2","title":"Old","state":"DISCONTINUED"}],"nextCursor":"I2"}`,
	})
	got, err := sources.OfferAvailability{Client: c}.ListCatalog(context.Background(), "c1", 2)
	if err != nil {
		t.Fatal(err)
	}
	same(t, "catalog", got, readmodel.Page[readmodel.CatalogRow]{Next: "I2", Items: []readmodel.CatalogRow{
		{CatalogItemID: "I1", Title: "Kettle", Hazmat: true},
		{CatalogItemID: "I2", Title: "Old", Discontinued: true},
	}})
}

func TestListingsMapTheNetworkContract(t *testing.T) {
	c, _ := serve(t, map[string]string{
		"/listings": `{"listings":[{"listingId":"L1","catalogItemId":"I1","nodeId":"N","state":"SUPPRESSED",
			"availability":{"availableQty":4,"basis":"PHYSICAL","asOf":"2026-10-25T09:00:00+02:00"}},
			{"listingId":"L2","catalogItemId":"I1","nodeId":"N","state":"LIVE","availability":{"availableQty":0,"basis":"UNKNOWN"}}]}`,
	})
	got, err := sources.OfferAvailability{Client: c}.ListListings(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	same(t, "listings", got, readmodel.Page[readmodel.ListingRow]{Items: []readmodel.ListingRow{
		{ListingID: "L1", CatalogItemID: "I1", NodeID: "N", Suppressed: true, AvailableQty: 4, AsOf: time.Date(2026, 10, 25, 7, 0, 0, 0, time.UTC)},
		{ListingID: "L2", CatalogItemID: "I1", NodeID: "N"},
	}})
}

func TestPricingMapsTheStorefrontContract(t *testing.T) {
	c, _ := serve(t, map[string]string{
		"/item-prices": `{"items":[{"catalogItemId":"I1","state":"ACTIVE","listPrice":{"amountMinor":900,"currency":"BRL"},
			"effectivePrice":{"amountMinor":800,"currency":"BRL"},"version":3,"sales":[]},
			{"catalogItemId":"I2","state":"WITHDRAWN","listPrice":{"amountMinor":1,"currency":"BRL"},"effectivePrice":{"amountMinor":1,"currency":"BRL"},"version":5}],"next":"I2"}`,
	})
	got, err := sources.Pricing{Client: c}.ListItemPrices(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	same(t, "prices", got, readmodel.Page[readmodel.PriceRow]{Next: "I2", Items: []readmodel.PriceRow{
		{CatalogItemID: "I1", Currency: "BRL", ListPriceMinor: 900, EffectivePriceMinor: 800, Version: 3},
		{CatalogItemID: "I2", Withdrawn: true, Currency: "BRL", ListPriceMinor: 1, EffectivePriceMinor: 1, Version: 5},
	}})
}

func TestContentAndCategoriesMapTheStorefrontContract(t *testing.T) {
	c, _ := serve(t, map[string]string{
		"/product-contents": `{"items":[{"catalogItemId":"I1","state":"PUBLISHED","revision":2,"description":"d","bullets":["b"],
			"attributes":[{"name":"n","value":"v","unit":"u"}],"media":[{"mediaId":"m","url":"https://x.test/m","altText":"a","role":"PRIMARY"}],"primaryCategoryId":"c1"}],"next":null}`,
		"/categories": `{"version":4,"categories":[{"categoryId":"c1","name":"One","path":"One"},{"categoryId":"c2","parentId":"c1","name":"Two","path":"One/Two"}]}`,
	})
	pc := sources.ProductContent{Client: c}
	contents, err := pc.ListProductContents(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	same(t, "contents", contents, readmodel.Page[readmodel.ContentRow]{Items: []readmodel.ContentRow{{
		CatalogItemID: "I1", State: "PUBLISHED", Revision: 2,
		Published: productview.PublishedContent{
			Revision: 2, Description: "d", Bullets: []string{"b"}, CategoryID: "c1",
			Attributes: []productview.Attribute{{Name: "n", Value: "v", Unit: "u"}},
			Media:      []productview.Media{{MediaID: "m", URL: "https://x.test/m", AltText: "a", Role: "PRIMARY"}},
		},
	}}})
	tree, err := pc.ListCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	same(t, "tree", tree, readmodel.CategoryTree{Version: 4, Categories: []categoryview.Category{
		{ID: "c1", Name: "One", Path: "One"}, {ID: "c2", ParentID: "c1", Name: "Two", Path: "One/Two"},
	}})
}

func TestRetriesServerErrorsButNotClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"version":1,"categories":[]}`))
	}))
	defer srv.Close()
	pc := sources.ProductContent{Client: sources.Client{BaseURL: srv.URL, Pause: time.Millisecond}}
	if _, err := pc.ListCategories(context.Background()); err != nil || calls.Load() != 3 {
		t.Fatalf("err = %v after %d calls", err, calls.Load())
	}
	missing, hits := serve(t, nil)
	_, err := sources.Pricing{Client: missing}.ListItemPrices(context.Background(), "", 0)
	if !errors.Is(err, sources.ErrUnexpectedStatus) || hits.Load() != 1 {
		t.Fatalf("404: err = %v after %d calls", err, hits.Load())
	}
}

func TestGivesUpOnBadBodiesAndPersistentFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/categories" {
			_, _ = w.Write([]byte(`not json`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	pc := sources.ProductContent{Client: sources.Client{BaseURL: srv.URL, Pause: time.Millisecond}}
	if _, err := pc.ListCategories(context.Background()); err == nil {
		t.Fatal("a body that is not JSON must fail")
	}
	if _, err := pc.ListProductContents(context.Background(), "", 0); !errors.Is(err, sources.ErrUnexpectedStatus) {
		t.Fatalf("persistent 503: %v", err)
	}
}

func TestUnreachableAndCancelled(t *testing.T) {
	dead := sources.Pricing{Client: sources.Client{BaseURL: "http://127.0.0.1:1", Pause: time.Millisecond}}
	if _, err := dead.ListItemPrices(context.Background(), "", 0); err == nil {
		t.Fatal("an unreachable service must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dead.ListItemPrices(ctx, "", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
