package mcp_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/IQVO/product-discovery/internal/adapters/inbound/mcp"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

// harness is a connected in-memory MCP client over the real server wired to
// the read use cases over the in-memory read model, exactly as cmd/mcp wires
// them (minus Postgres). The write side (the Consumer) exists ONLY here, to
// seed the projections the way the Kafka consumers would: the MCP surface
// itself has none.
type harness struct {
	t        *testing.T
	session  *sdk.ClientSession
	reads    usecases.Queries
	consumer usecases.Consumer
	n        int
}

// newHarness seeds a two-level category tree, two purchasable kettles (I-1 at
// 3000, I-2 at 8000) and one stub (S-1: a price arrived before the item
// master announced the item, so it must stay invisible).
func newHarness(t *testing.T) *harness {
	t.Helper()
	s := memory.NewStore()
	d := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	h := &harness{t: t, consumer: usecases.Consumer{Deps: d}}
	h.must(h.consumer.ApplyCategoryTreePublished(context.Background(), h.meta(), usecases.CategoryTreePublished{Version: 2, Categories: []categoryview.Category{
		{ID: "home", Name: "Home", Path: "Home"}, {ID: "kettles", ParentID: "home", Name: "Kettles", Path: "Home/Kettles"},
	}}))
	h.item("I-1", "Steel Kettle", 3000)
	h.item("I-2", "Glass Kettle", 8000)
	h.must(h.consumer.ApplyItemPriceChanged(context.Background(), h.meta(), usecases.ItemPriceChanged{CatalogItemID: "S-1", Price: productview.PriceChanged{
		Currency: "USD", ListPriceMinor: 900, EffectivePriceMinor: 900, Version: 1,
	}}))
	reads := usecases.Queries{Deps: usecases.Deps{Products: s.Products(), Categories: s.Categories(), Search: s.Search()}}
	h.reads = reads
	h.session = connectSession(t, inboundmcp.Deps{Reads: reads})
	return h
}

func connectSession(t *testing.T, deps inboundmcp.Deps) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := inboundmcp.NewServer(deps).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func (h *harness) meta() usecases.Meta {
	h.n++
	return usecases.Meta{EventID: fmt.Sprintf("ev-%d", h.n), Type: "test"}
}

func (h *harness) must(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

// item feeds the events that make id a purchasable kettle.
func (h *harness) item(id, title string, price int64) {
	h.t.Helper()
	bg, c := context.Background(), h.consumer
	h.must(c.ApplyCatalogItemCreated(bg, h.meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title, Hazmat: true}))
	h.must(c.ApplyListingAvailabilityChanged(bg, h.meta(), usecases.ListingAvailabilityChanged{
		ListingID: "L-" + id, CatalogItemID: id, NodeID: "N", AvailableQty: 2, AsOf: t0,
	}))
	h.must(c.ApplyProductContentPublished(bg, h.meta(), usecases.ProductContentPublished{CatalogItemID: id, Content: productview.PublishedContent{
		Revision: 1, Description: "Boils water", Bullets: []string{"Fast"}, CategoryID: "kettles",
		Attributes: []productview.Attribute{{Name: "capacity", Value: "1.7", Unit: "l"}, {Name: "colour", Value: "red"}},
		Media:      []productview.Media{{MediaID: "m1", URL: "https://cdn.example.test/m1.jpg", AltText: "alt", Role: "PRIMARY"}},
	}}))
	h.must(c.ApplyItemPriceChanged(bg, h.meta(), usecases.ItemPriceChanged{CatalogItemID: id, Price: productview.PriceChanged{
		Currency: "USD", ListPriceMinor: price + 500, EffectivePriceMinor: price, Version: 1,
	}}))
}

// call invokes a tool and fails the test on a TRANSPORT/protocol error;
// tool-level failures come back as res.IsError.
func (h *harness) call(name string, args map[string]any) *sdk.CallToolResult {
	h.t.Helper()
	res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: transport/protocol error (want a tool result): %v", name, err)
	}
	return res
}

// ok calls a tool, requires success, and returns its structured content.
func (h *harness) ok(name string, args map[string]any) map[string]any {
	h.t.Helper()
	res := h.call(name, args)
	if res.IsError {
		h.t.Fatalf("%s returned a tool error: %s", name, text(res))
	}
	sc, isMap := res.StructuredContent.(map[string]any)
	if !isMap {
		h.t.Fatalf("%s: structured content = %#v, want an object", name, res.StructuredContent)
	}
	return sc
}

// failWith calls a tool and requires an isError result whose text starts with
// the slug want.
func (h *harness) failWith(name string, args map[string]any, want string) {
	h.t.Helper()
	res := h.call(name, args)
	if !res.IsError {
		h.t.Fatalf("%s: expected an isError tool result, got %#v", name, res.StructuredContent)
	}
	if got := text(res); !strings.HasPrefix(got, want+": ") {
		h.t.Fatalf("%s: error text %q does not start with %q", name, got, want+": ")
	}
}

func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, isText := c.(*sdk.TextContent); isText {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
