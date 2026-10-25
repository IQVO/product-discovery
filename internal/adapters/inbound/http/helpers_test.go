package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

type env struct {
	t        *testing.T
	consumer usecases.Consumer
	ready    *httpadapter.Readiness
	srv      *httptest.Server
	n        int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := memory.NewStore()
	deps := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	ready := httpadapter.NewReadiness(0, 0)
	srv := httptest.NewServer(httpadapter.NewRouter(&httpadapter.Server{Queries: usecases.Queries{Deps: deps}, Readiness: ready}))
	t.Cleanup(srv.Close)
	return &env{t: t, consumer: usecases.Consumer{Deps: deps}, ready: ready, srv: srv}
}

func (e *env) meta() usecases.Meta {
	e.n++
	return usecases.Meta{EventID: fmt.Sprintf("ev-%d", e.n), Type: "test"}
}

func (e *env) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

// item feeds the events that make id a purchasable kettle.
func (e *env) item(id, title string, price int64) {
	e.t.Helper()
	bg, c := context.Background(), e.consumer
	e.must(c.ApplyCatalogItemCreated(bg, e.meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title, Hazmat: true}))
	e.must(c.ApplyListingAvailabilityChanged(bg, e.meta(), usecases.ListingAvailabilityChanged{
		ListingID: "L-" + id, CatalogItemID: id, NodeID: "N", AvailableQty: 2, AsOf: t0,
	}))
	e.must(c.ApplyProductContentPublished(bg, e.meta(), usecases.ProductContentPublished{CatalogItemID: id, Content: productview.PublishedContent{
		Revision: 1, Description: "Boils water", Bullets: []string{"Fast"}, CategoryID: "kettles",
		Attributes: []productview.Attribute{{Name: "capacity", Value: "1.7", Unit: "l"}, {Name: "colour", Value: "red"}},
		Media:      []productview.Media{{MediaID: "m1", URL: "https://cdn.example.test/m1.jpg", AltText: "alt", Role: "PRIMARY"}},
	}}))
	e.must(c.ApplyItemPriceChanged(bg, e.meta(), usecases.ItemPriceChanged{CatalogItemID: id, Price: productview.PriceChanged{
		Currency: "USD", ListPriceMinor: price + 500, EffectivePriceMinor: price, Version: 1,
	}}))
}

func (e *env) tree() {
	e.t.Helper()
	e.must(e.consumer.ApplyCategoryTreePublished(context.Background(), e.meta(), usecases.CategoryTreePublished{Version: 2, Categories: []categoryview.Category{
		{ID: "home", Name: "Home", Path: "Home"}, {ID: "kettles", ParentID: "home", Name: "Kettles", Path: "Home/Kettles"},
	}}))
}

type reply struct {
	Status int
	Header http.Header
	Body   string
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Body), &m); err != nil {
		t.Fatalf("body is not JSON: %q (%v)", r.Body, err)
	}
	return m
}

func (e *env) get(path string) reply {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+path, strings.NewReader(""))
	if err != nil {
		e.t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{Status: resp.StatusCode, Header: resp.Header, Body: string(b)}
}

func (e *env) wantStatus(r reply, status int) {
	e.t.Helper()
	if r.Status != status {
		e.t.Fatalf("status = %d, want %d: %s", r.Status, status, r.Body)
	}
}

func (e *env) wantProblem(r reply, status int, slug string) {
	e.t.Helper()
	e.wantStatus(r, status)
	if ct := r.Header.Get("Content-Type"); ct != "application/problem+json" {
		e.t.Fatalf("content-type = %q", ct)
	}
	if got := r.json(e.t)["type"]; got != "/problems/"+slug {
		e.t.Fatalf("problem type = %v, want /problems/%s (%s)", got, slug, r.Body)
	}
}
