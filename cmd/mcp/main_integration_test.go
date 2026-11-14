//go:build integration

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

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
// fold use cases over the real Postgres adapters), so the MCP server below
// reads rows it did not write.
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

// bootServer runs the REAL entry point in-process, with nothing but the
// environment a deployment sets (no Kafka variable: the server has none), and
// stops it on cleanup.
func bootServer(t *testing.T, dsn string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("MCP_ADDR", addr)
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
			t.Error("the MCP server did not stop")
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
	t.Fatal("the MCP server never became healthy")
	return ""
}

// TestRealWiringServerReadsPostgresOverTheWire proves tools/list -> a tool call
// -> read use cases -> Postgres on the shipped entry point: the exact tool set,
// every tool L1 and read-only, one call of each tool, the resource, the prompt
// and the open /healthz.
func TestRealWiringServerReadsPostgresOverTheWire(t *testing.T) {
	dsn := pgtest.URL(t) // a fresh, already migrated database: the boot migration must be a no-op
	seedPostgres(t, dsn)
	addr := bootServer(t, dsn)

	ctx := context.Background()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	assertToolSet(t, session)
	assertToolReads(t, session)

	res, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "warehouse://product-discovery/products/I-1"})
	if err != nil || len(res.Contents) != 1 {
		t.Fatalf("resource = %v, %v", res, err)
	}
	if _, err := session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "find_products", Arguments: map[string]string{"query": "kettle"}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
}

// assertToolSet requires tools/list to return exactly the golden tool set, every
// tool read-only and L1.
func assertToolSet(t *testing.T, session *sdk.ClientSession) {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Meta["com.warehouse/authority"] != "L1" {
			t.Fatalf("tool %q is not read-only L1: %+v %v", tool.Name, tool.Annotations, tool.Meta)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "get_product,search_products" {
		t.Fatalf("tools = %v, want exactly get_product and search_products", names)
	}
}

// assertToolReads calls each tool once against the seeded Postgres, and one
// unknown id for the not-found answer.
func assertToolReads(t *testing.T, session *sdk.ClientSession) {
	t.Helper()
	got, failure := toolCall(t, session, "get_product", map[string]any{"catalogItemId": "I-1"})
	price, _ := got["effectivePrice"].(map[string]any)
	if failure != "" || got["title"] != "Steel Kettle" || got["purchasable"] != true || price["amountMinor"] != float64(9990) || price["currency"] != "BRL" {
		t.Fatalf("get_product = %v / %q", got, failure)
	}
	got, failure = toolCall(t, session, "search_products", map[string]any{"q": "ketle"}) // typo: the pg_trgm fallback
	items, _ := got["items"].([]any)
	if failure != "" || len(items) != 1 || items[0].(map[string]any)["catalogItemId"] != "I-1" {
		t.Fatalf("search_products = %v / %q", got, failure)
	}
	if _, failure = toolCall(t, session, "get_product", map[string]any{"catalogItemId": "GHOST"}); failure != `not-found: no product exists with catalogItemId "GHOST"` {
		t.Fatalf("an unknown product must be a not-found tool error, got %q", failure)
	}
}
