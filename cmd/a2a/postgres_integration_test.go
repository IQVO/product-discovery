//go:build integration

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Shutdown()
	os.Exit(code)
}

// seedPostgres writes the read model the way cmd/api's consumers do (the real
// fold use cases over the real Postgres adapters), so the agent below reads
// rows it did not write.
func seedPostgres(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	c := usecases.Consumer{Deps: usecases.Deps{
		Products: postgres.NewProductRepository(pool), Categories: postgres.NewCategoryRepository(pool),
		Listings: postgres.NewListingItemMap(pool), Inbox: postgres.NewInbox(pool), UoW: postgres.NewUnitOfWork(pool),
		Search: postgres.NewSearch(pool),
	}}
	n := 0
	meta := func() usecases.Meta { n++; return usecases.Meta{EventID: fmt.Sprintf("it-%d", n), Type: "test"} }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.ApplyCategoryTreePublished(ctx, meta(), usecases.CategoryTreePublished{Version: 1, Categories: []categoryview.Category{
		{ID: "kettles", Name: "Kettles", Path: "Kettles"},
	}}))
	must(c.ApplyCatalogItemCreated(ctx, meta(), usecases.CatalogItemCreated{CatalogItemID: "I-1", Title: "Steel Kettle"}))
	must(c.ApplyListingAvailabilityChanged(ctx, meta(), usecases.ListingAvailabilityChanged{ListingID: "L-1", CatalogItemID: "I-1", NodeID: "N", AvailableQty: 2, AsOf: t0}))
	must(c.ApplyProductContentPublished(ctx, meta(), usecases.ProductContentPublished{CatalogItemID: "I-1", Content: productview.PublishedContent{
		Revision: 1, Description: "Boils water fast", CategoryID: "kettles",
	}}))
	must(c.ApplyItemPriceChanged(ctx, meta(), usecases.ItemPriceChanged{CatalogItemID: "I-1", Price: productview.PriceChanged{Currency: "BRL", ListPriceMinor: 12990, EffectivePriceMinor: 9990, Version: 1}}))
}

// bootAgent runs the REAL entry point in-process, with nothing but the
// environment a deployment sets (no Kafka variable: the agent has none), and
// stops it on cleanup.
func bootAgent(t *testing.T, dsn string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("A2A_ADDR", addr)
	t.Setenv("A2A_AGENT_URL", "http://"+addr+"/")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run returned %v on shutdown", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("the agent did not stop")
		}
	})
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if resp, err := http.Get("http://" + addr + "/healthz"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return addr
			}
		}
	}
	t.Fatal("the agent never became healthy")
	return ""
}

// TestRealWiringAgentReadsPostgresOverTheWire proves card -> JSON-RPC wire ->
// read use cases -> Postgres on the shipped binary: discovery through the
// fetched card, both skills, and one in-band problem.
func TestRealWiringAgentReadsPostgresOverTheWire(t *testing.T) {
	dsn := pgtest.URL(t)
	seedPostgres(t, dsn)
	addr := bootAgent(t, dsn)

	client := clientFor(t, "http://"+addr)
	data, problem := skillData(t, client, "get_product", map[string]any{"catalogItemId": "I-1"})
	p, _ := data["product"].(map[string]any)
	if problem != nil || p["title"] != "Steel Kettle" || p["purchasable"] != true || p["categoryPath"].([]any)[0] != "Kettles" {
		t.Fatalf("get_product = %v / %v", data, problem)
	}
	if price := p["effectivePrice"].(map[string]any); price["amountMinor"] != float64(9990) || price["currency"] != "BRL" {
		t.Fatalf("price = %v", price)
	}

	data, problem = skillData(t, client, "search_products", map[string]any{"q": "ketle"}) // typo: the pg_trgm fallback
	hits, _ := data["search"].(map[string]any)["hits"].([]any)
	if problem != nil || len(hits) != 1 || hits[0].(map[string]any)["catalogItemId"] != "I-1" {
		t.Fatalf("search_products = %v / %v", data, problem)
	}

	_, problem = skillData(t, client, "get_product", map[string]any{"catalogItemId": "GHOST"})
	if problem == nil || problem["status"] != float64(404) {
		t.Fatalf("an unknown product must be answered in-band as a 404, got %v", problem)
	}
	_, problem = skillData(t, client, "search_products", map[string]any{"sort": "cheapest"})
	if problem == nil || problem["status"] != float64(422) {
		t.Fatalf("a bad sort must be answered in-band as a 422, got %v", problem)
	}
}
