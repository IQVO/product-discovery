package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
	inboundmcp "github.com/IQVO/product-discovery/internal/adapters/inbound/mcp"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

// seed fills a memory store through the real fold use cases: a category tree,
// two purchasable kettles and one stub. The memory adapter is a test double: it
// is imported by this _test file only, never by the binary
// (TestRuntimeBinaryHasNoInMemoryAdapters).
func seed(t *testing.T) usecases.Queries {
	t.Helper()
	s := memory.NewStore()
	d := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	c, bg, n := usecases.Consumer{Deps: d}, context.Background(), 0
	meta := func() usecases.Meta { n++; return usecases.Meta{EventID: fmt.Sprintf("ev-%d", n), Type: "test"} }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.ApplyCategoryTreePublished(bg, meta(), usecases.CategoryTreePublished{Version: 2, Categories: []categoryview.Category{
		{ID: "home", Name: "Home", Path: "Home"}, {ID: "kettles", ParentID: "home", Name: "Kettles", Path: "Home/Kettles"},
	}}))
	for id, title := range map[string]string{"I-1": "Steel Kettle", "I-2": "Glass Kettle"} {
		price := int64(3000)
		if id == "I-2" {
			price = 8000
		}
		must(c.ApplyCatalogItemCreated(bg, meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title, Hazmat: id == "I-1", Fragile: id == "I-2"}))
		must(c.ApplyListingAvailabilityChanged(bg, meta(), usecases.ListingAvailabilityChanged{ListingID: "L-" + id, CatalogItemID: id, NodeID: "N", AvailableQty: 2, AsOf: t0}))
		must(c.ApplyProductContentPublished(bg, meta(), usecases.ProductContentPublished{CatalogItemID: id, Content: productview.PublishedContent{
			Revision: 1, Description: "Boils water", Bullets: []string{"Fast"}, CategoryID: "kettles",
			Attributes: []productview.Attribute{{Name: "capacity", Value: "1.7", Unit: "l"}, {Name: "colour", Value: "red"}},
			Media:      []productview.Media{{MediaID: "m1", URL: "https://cdn.example.test/m1.jpg", AltText: "alt", Role: "PRIMARY"}},
		}}))
		must(c.ApplyItemPriceChanged(bg, meta(), usecases.ItemPriceChanged{CatalogItemID: id, Price: productview.PriceChanged{Currency: "USD", ListPriceMinor: price + 500, EffectivePriceMinor: price, Version: 1}}))
	}
	must(c.ApplyItemPriceChanged(bg, meta(), usecases.ItemPriceChanged{CatalogItemID: "S-1", Price: productview.PriceChanged{Currency: "USD", ListPriceMinor: 900, EffectivePriceMinor: 900, Version: 1}}))
	// The server's own wiring: the read side only.
	return usecases.Queries{Deps: usecases.Deps{Products: s.Products(), Categories: s.Categories(), Search: s.Search()}}
}

// mcpSession serves the real router (real handler, real middleware) over the
// read model and returns a Streamable HTTP client session.
func mcpSession(t *testing.T, queries usecases.Queries) *sdk.ClientSession {
	t.Helper()
	srv := httptest.NewServer(newRouter(inboundmcp.Handler(inboundmcp.NewServer(inboundmcp.Deps{Reads: queries})), quietLogger(), "product-discovery-mcp"))
	t.Cleanup(srv.Close)
	session, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func restGet(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("GET %s: not JSON: %q", path, rec.Body.String())
	}
	return rec.Code, m
}

// toolCall returns the structured content of a successful tool call, or the
// error text of an isError result.
func toolCall(t *testing.T, s *sdk.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return nil, b.String()
	}
	sc, _ := res.StructuredContent.(map[string]any)
	return sc, ""
}

// TestParity_TheToolsExposeExactlyWhatRESTExposes drives the REST router and
// the MCP server over the SAME read model and compares the payloads field for
// field: a tool never exposes a field REST withholds, never hides one REST
// shows, and classifies a failure with the REST problem slug. The two DTO sets
// are duplicated (the MCP adapter may not import the REST one), so this test is
// what stops them drifting.
func TestParity_TheToolsExposeExactlyWhatRESTExposes(t *testing.T) {
	queries := seed(t)
	p := parity{
		rest:    inboundhttp.NewRouter(&inboundhttp.Server{Queries: queries}, slog.New(slog.DiscardHandler), ""),
		session: mcpSession(t, queries),
	}

	t.Run("get product", func(t *testing.T) {
		for _, id := range []string{"I-1", "I-2"} {
			p.same(t, "/products/"+id, "get_product", map[string]any{"catalogItemId": id})
		}
	})

	t.Run("search", func(t *testing.T) {
		for _, args := range []map[string]any{
			{"q": "kettles", "sort": "title", "limit": 1},
			{"q": "ketle", "sort": "price-desc"},
			{"categoryId": "home", "inStock": true, "priceMax": 5000},
			{"sort": "price-asc"},
			{"q": "nothing-matches-this"},
		} {
			p.same(t, "/products?"+query(args), "search_products", args)
		}
	})

	t.Run("the cursor of one surface pages the other", func(t *testing.T) {
		args := map[string]any{"q": "kettles", "sort": "title", "limit": 1}
		_, first := restGet(t, p.rest, "/products?"+query(args))
		args["cursor"] = first["nextCursor"]
		p.same(t, "/products?"+query(args), "search_products", args)
	})

	t.Run("failures carry the REST problem slug", func(t *testing.T) {
		p.sameSlug(t, "/products/GHOST", "get_product", map[string]any{"catalogItemId": "GHOST"})
		p.sameSlug(t, "/products/S-1", "get_product", map[string]any{"catalogItemId": "S-1"})
		p.sameSlug(t, "/products?sort=cheapest", "search_products", map[string]any{"sort": "cheapest"})
		p.sameSlug(t, "/products?priceMin=9&priceMax=8", "search_products", map[string]any{"priceMin": 9, "priceMax": 8})
		p.sameSlug(t, "/products?limit=0", "search_products", map[string]any{"limit": 0})
		p.sameSlug(t, "/products?limit=501", "search_products", map[string]any{"limit": 501})
		p.sameSlug(t, "/products?cursor=not-a-cursor", "search_products", map[string]any{"cursor": "not-a-cursor"})
	})
}

// parity holds the two surfaces over the one read model.
type parity struct {
	rest    http.Handler
	session *sdk.ClientSession
}

// same requires the REST body and the tool's structured content to be equal.
func (p parity) same(t *testing.T, restPath, tool string, args map[string]any) {
	t.Helper()
	status, want := restGet(t, p.rest, restPath)
	got, failure := toolCall(t, p.session, tool, args)
	if status != 200 || failure != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: REST %d %v\nMCP  %v (%s)", restPath, status, want, got, failure)
	}
}

// sameSlug requires the REST problem and the tool error to carry one slug.
func (p parity) sameSlug(t *testing.T, restPath, tool string, args map[string]any) {
	t.Helper()
	status, body := restGet(t, p.rest, restPath)
	_, failure := toolCall(t, p.session, tool, args)
	slug := strings.TrimPrefix(fmt.Sprint(body["type"]), "/problems/")
	if status < 400 || !strings.HasPrefix(failure, slug+": ") {
		t.Fatalf("%s: REST %d %v, MCP %q: want the slug %q", restPath, status, body, failure, slug)
	}
}

func query(args map[string]any) string {
	v := url.Values{}
	for k, val := range args {
		v.Set(k, fmt.Sprint(val))
	}
	return v.Encode()
}
