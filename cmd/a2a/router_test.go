package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
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
	// The agent's own wiring: the read side only.
	return usecases.Queries{Deps: usecases.Deps{Products: s.Products(), Categories: s.Categories(), Search: s.Search()}}
}

func newTestAgent(t *testing.T) http.Handler {
	t.Helper()
	return newAgent(seed(t), testAgentURL, "test")
}

func TestRouter_HealthzServesOK(t *testing.T) {
	h := newTestAgent(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != `{"status":"ok"}` || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET /healthz: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestRouter_ServesTheAgentCardAtTheWellKnownPath(t *testing.T) {
	h := newTestAgent(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET agent card: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"name":"product-discovery"`, testAgentURL, `"search_products"`, `"get_product"`, `"version":"test"`} {
		if !strings.Contains(body, want) {
			t.Errorf("agent card lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "securitySchemes") {
		t.Errorf("the card must declare no security schemes: %s", body)
	}
}

// TestRouter_BothMountsReachTheJSONRPCHandler pins the fleet's
// <CTX>_A2A_ENDPOINT convention: the endpoint answers at "/" and at "/a2a", and
// the use cases behind it are the wired ones (the seeded product is read).
func TestRouter_BothMountsReachTheJSONRPCHandler(t *testing.T) {
	h := newTestAgent(t)
	for _, path := range []string{"/", "/a2a"} {
		t.Run(path, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"data":{"skill":"get_product","arguments":{"catalogItemId":"I-1"}}}]}}}`
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("A2A-Version", "1.0")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			raw, _ := io.ReadAll(rec.Body)
			if rec.Code == http.StatusNotFound || !strings.Contains(string(raw), `"jsonrpc"`) {
				t.Fatalf("POST %s: %d %q does not look like a JSON-RPC reply", path, rec.Code, raw)
			}
			if !strings.Contains(string(raw), `"title":"Steel Kettle"`) {
				t.Fatalf("POST %s: reply %q lacks the seeded product read through the wired use case", path, raw)
			}
		})
	}
}

// agentClient serves h on a test server and runs clientFor against it.
func agentClient(t *testing.T, h http.Handler) *a2aclient.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return clientFor(t, srv.URL)
}

// clientFor runs the discovery flow a delegating agent runs against the real
// router: fetch the card, build a client from it, call a skill.
func clientFor(t *testing.T, base string) *a2aclient.Client {
	t.Helper()
	res, err := http.Get(base + a2asrv.WellKnownAgentCardPath)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("card fetch = %v / %v", res, err)
	}
	defer func() { _ = res.Body.Close() }()
	var card a2a.AgentCard
	if err := json.NewDecoder(res.Body).Decode(&card); err != nil {
		t.Fatal(err)
	}
	// The card advertises the configured URL, not the test server's: point the
	// client at the test server the way a gateway-rewritten route would.
	card.SupportedInterfaces[0].URL = base + "/a2a"
	client, err := a2aclient.NewFromCard(context.Background(), &card)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// skillData sends one skill request and returns the envelope's data and problem.
func skillData(t *testing.T, c *a2aclient.Client, skill string, args map[string]any) (data, problem map[string]any) {
	t.Helper()
	resp, err := c.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": skill, "arguments": args}))})
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := resp.(*a2a.Message)
	if !ok || len(msg.Parts) < 2 {
		t.Fatalf("reply = %T", resp)
	}
	raw, _ := json.Marshal(msg.Parts[0].Content.(a2a.Data).Value)
	var env struct {
		Data    map[string]any `json:"data"`
		Problem map[string]any `json:"problem"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return env.Data, env.Problem
}

func TestRouter_ACardFetchedFromTheBinaryIsDialable(t *testing.T) {
	c := agentClient(t, newTestAgent(t))
	data, problem := skillData(t, c, "search_products", map[string]any{"q": "kettle", "sort": "title"})
	hits := data["search"].(map[string]any)["hits"].([]any)
	if problem != nil || len(hits) != 2 {
		t.Fatalf("data = %v problem = %v", data, problem)
	}
}

// ---- parity with the REST adapter -------------------------------------------

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

// TestParity_TheAgentExposesExactlyWhatRESTExposes drives the REST router and
// the A2A agent over the SAME read model and compares the payloads
// field for field: the agent never exposes a field REST withholds, never hides
// one REST shows, and classifies a failure the way REST does. The two DTO sets
// are duplicated (the A2A adapter may not import the REST one), so this test is
// what stops them drifting.
func TestParity_TheAgentExposesExactlyWhatRESTExposes(t *testing.T) {
	queries := seed(t)
	rest := inboundhttp.NewRouter(&inboundhttp.Server{Queries: queries}, slog.New(slog.DiscardHandler), "")
	agent := agentClient(t, newAgent(queries, testAgentURL, "test"))

	t.Run("get product", func(t *testing.T) {
		for _, id := range []string{"I-1", "I-2"} {
			status, want := restGet(t, rest, "/products/"+id)
			data, problem := skillData(t, agent, "get_product", map[string]any{"catalogItemId": id})
			if status != 200 || problem != nil || !reflect.DeepEqual(data["product"], any(want)) {
				t.Fatalf("%s: REST %d %v\nA2A  %v (problem %v)", id, status, want, data["product"], problem)
			}
		}
	})

	t.Run("search", func(t *testing.T) {
		cases := []map[string]any{
			{"q": "kettles", "sort": "title", "limit": 1},
			{"q": "ketle", "sort": "price-desc"},
			{"categoryId": "home", "inStock": true, "priceMax": 5000},
			{"sort": "price-asc"},
			{"q": "nothing-matches-this"},
		}
		for _, args := range cases {
			status, want := restGet(t, rest, "/products?"+query(args))
			data, problem := skillData(t, agent, "search_products", args)
			if status != 200 || problem != nil || !reflect.DeepEqual(data["search"], any(want)) {
				t.Fatalf("%v: REST %d %v\nA2A  %v (problem %v)", args, status, want, data["search"], problem)
			}
		}
	})

	t.Run("failures are classified like REST", func(t *testing.T) {
		restStatus, restBody := restGet(t, rest, "/products/GHOST")
		_, problem := skillData(t, agent, "get_product", map[string]any{"catalogItemId": "GHOST"})
		assertSameClass(t, restStatus, restBody, problem)

		restStatus, restBody = restGet(t, rest, "/products?sort=cheapest")
		_, problem = skillData(t, agent, "search_products", map[string]any{"sort": "cheapest"})
		assertSameClass(t, restStatus, restBody, problem)

		restStatus, restBody = restGet(t, rest, "/products?limit=0")
		_, problem = skillData(t, agent, "search_products", map[string]any{"limit": 0})
		assertSameClass(t, restStatus, restBody, problem)
	})
}

func assertSameClass(t *testing.T, restStatus int, restBody, problem map[string]any) {
	t.Helper()
	if problem == nil || int(problem["status"].(float64)) != restStatus || problem["type"] != restBody["type"] || problem["title"] != restBody["title"] {
		t.Fatalf("REST %d %v, A2A %v", restStatus, restBody, problem)
	}
}

func query(args map[string]any) string {
	v := url.Values{}
	for k, val := range args {
		v.Set(k, fmt.Sprint(val))
	}
	return v.Encode()
}
