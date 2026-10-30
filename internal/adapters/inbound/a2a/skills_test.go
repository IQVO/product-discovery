package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

// fixture is a seeded read model: a two-level category tree, two purchasable
// kettles (I-1 at 3000, I-2 at 8000) and one stub (S-1: a price arrived before
// the item master announced the item, so it must stay invisible). The memory
// store is a test double; the adapter only ever sees the read use cases.
type fixture struct {
	t        *testing.T
	store    *memory.Store
	consumer usecases.Consumer
	n        int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s := memory.NewStore()
	d := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	f := &fixture{t: t, store: s, consumer: usecases.Consumer{Deps: d}}
	f.must(f.consumer.ApplyCategoryTreePublished(context.Background(), f.meta(), usecases.CategoryTreePublished{Version: 2, Categories: []categoryview.Category{
		{ID: "home", Name: "Home", Path: "Home"}, {ID: "kettles", ParentID: "home", Name: "Kettles", Path: "Home/Kettles"},
	}}))
	f.item("I-1", "Steel Kettle", 3000)
	f.item("I-2", "Glass Kettle", 8000)
	f.must(f.consumer.ApplyItemPriceChanged(context.Background(), f.meta(), usecases.ItemPriceChanged{CatalogItemID: "S-1", Price: productview.PriceChanged{
		Currency: "USD", ListPriceMinor: 900, EffectivePriceMinor: 900, Version: 1,
	}}))
	return f
}

// deps is what cmd/a2a wires: the read side only (no inbox, no unit of work).
func (f *fixture) deps() Deps {
	s := f.store
	return Deps{Queries: usecases.Queries{Deps: usecases.Deps{Products: s.Products(), Categories: s.Categories(), Search: s.Search()}}}
}

func (f *fixture) meta() usecases.Meta {
	f.n++
	return usecases.Meta{EventID: fmt.Sprintf("ev-%d", f.n), Type: "test"}
}

func (f *fixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) item(id, title string, price int64) {
	f.t.Helper()
	bg, c := context.Background(), f.consumer
	f.must(c.ApplyCatalogItemCreated(bg, f.meta(), usecases.CatalogItemCreated{CatalogItemID: id, Title: title, Hazmat: true}))
	f.must(c.ApplyListingAvailabilityChanged(bg, f.meta(), usecases.ListingAvailabilityChanged{
		ListingID: "L-" + id, CatalogItemID: id, NodeID: "N", AvailableQty: 2, AsOf: t0,
	}))
	f.must(c.ApplyProductContentPublished(bg, f.meta(), usecases.ProductContentPublished{CatalogItemID: id, Content: productview.PublishedContent{
		Revision: 1, Description: "Boils water", Bullets: []string{"Fast"}, CategoryID: "kettles",
		Attributes: []productview.Attribute{{Name: "capacity", Value: "1.7", Unit: "l"}, {Name: "colour", Value: "red"}},
		Media:      []productview.Media{{MediaID: "m1", URL: "https://cdn.example.test/m1.jpg", AltText: "alt", Role: "PRIMARY"}},
	}}))
	f.must(c.ApplyItemPriceChanged(bg, f.meta(), usecases.ItemPriceChanged{CatalogItemID: id, Price: productview.PriceChanged{
		Currency: "USD", ListPriceMinor: price + 500, EffectivePriceMinor: price, Version: 1,
	}}))
}

// ---- the wire: JSON-RPC over HTTP, driven by the SDK's own client ----------

// wireEnvelope is a TEST-LOCAL copy of the answer contract. If the adapter's
// envelope drifts, this suite fails instead of silently following it.
type wireEnvelope struct {
	Skill string `json:"skill"`
	Data  *struct {
		Product *wireProduct `json:"product"`
		Search  *wireSearch  `json:"search"`
	} `json:"data"`
	Problem *wireProblem `json:"problem"`
}

type wireProblem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

type wireMoney struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type wireProduct struct {
	CatalogItemID  string          `json:"catalogItemId"`
	Title          string          `json:"title"`
	Hazmat         bool            `json:"hazmat"`
	Description    string          `json:"description"`
	Bullets        []string        `json:"bullets"`
	CategoryID     string          `json:"categoryId"`
	CategoryPath   []string        `json:"categoryPath"`
	ListPrice      *wireMoney      `json:"listPrice"`
	EffectivePrice *wireMoney      `json:"effectivePrice"`
	InStock        bool            `json:"inStock"`
	Purchasable    bool            `json:"purchasable"`
	Attributes     []wireAttribute `json:"attributes"`
	Media          []wireMedia     `json:"media"`
}

type wireAttribute struct {
	Name  string  `json:"name"`
	Value string  `json:"value"`
	Unit  *string `json:"unit"`
}

type wireMedia struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

type wireSearch struct {
	Hits []struct {
		CatalogItemID  string     `json:"catalogItemId"`
		EffectivePrice *wireMoney `json:"effectivePrice"`
		CategoryPath   []string   `json:"categoryPath"`
	} `json:"hits"`
	Facets              []struct{ Name string } `json:"facets"`
	CategorySuggestions []struct {
		CategoryID string `json:"categoryId"`
	} `json:"categorySuggestions"`
	Next *string `json:"next"`
}

func (s *wireSearch) ids() []string {
	var out []string
	for _, h := range s.Hits {
		out = append(out, h.CatalogItemID)
	}
	return out
}

type wire struct {
	client *a2aclient.Client
}

func newWire(t *testing.T, deps Deps) *wire {
	t.Helper()
	srv := httptest.NewServer(Handler(NewExecutor(deps)))
	t.Cleanup(srv.Close)
	card := Card(Config{AgentURL: srv.URL + "/", Version: "test"})
	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("NewFromCard: %v", err)
	}
	return &wire{client: client}
}

// send sends one message and returns the agent's reply Message.
func (w *wire) send(t *testing.T, msg *a2a.Message) *a2a.Message {
	t.Helper()
	resp, err := w.client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	reply, ok := resp.(*a2a.Message)
	if !ok || len(reply.Parts) < 2 {
		t.Fatalf("reply = %T, want a message with a data and a text part", resp)
	}
	return reply
}

// call sends one message part and returns the decoded envelope and the
// one-line text summary of the agent's reply.
func (w *wire) call(t *testing.T, part *a2a.Part) (wireEnvelope, string) {
	t.Helper()
	msg := w.send(t, a2a.NewMessage(a2a.MessageRoleUser, part))
	data, ok := msg.Parts[0].Content.(a2a.Data)
	if !ok {
		t.Fatalf("first reply part is %T, want data", msg.Parts[0].Content)
	}
	raw, _ := json.Marshal(data.Value)
	var env wireEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("reply is not the skill envelope: %v (%s)", err, raw)
	}
	text, _ := msg.Parts[1].Content.(a2a.Text)
	return env, string(text)
}

func (w *wire) skill(t *testing.T, skill string, args map[string]any) (wireEnvelope, string) {
	t.Helper()
	return w.call(t, a2a.NewDataPart(map[string]any{"skill": skill, "arguments": args}))
}

func wantProblem(t *testing.T, env wireEnvelope, status int, slug string) {
	t.Helper()
	if env.Problem == nil {
		t.Fatalf("expected a %d problem, got data: %+v", status, env.Data)
	}
	if env.Problem.Status != status || env.Problem.Type != "/problems/"+slug || env.Data != nil {
		t.Fatalf("problem = %+v (data %v), want %d %s", env.Problem, env.Data, status, slug)
	}
}

func wantSearch(t *testing.T, env wireEnvelope) *wireSearch {
	t.Helper()
	if env.Problem != nil || env.Data == nil || env.Data.Search == nil {
		t.Fatalf("envelope = %+v (problem %+v), want a search result", env, env.Problem)
	}
	return env.Data.Search
}

func TestWireSearchProductsReturnsHitsFacetsSuggestionsAndCursor(t *testing.T) {
	w := newWire(t, newFixture(t).deps())

	env, text := w.skill(t, skillSearchProducts, map[string]any{"q": "kettles", "sort": "title", "limit": 1})
	page := wantSearch(t, env)
	assertFirstPage(t, env.Skill, page)
	if text != "1 products on this page (more pages: pass next as after)" {
		t.Fatalf("summary = %q", text)
	}

	env, text = w.skill(t, skillSearchProducts, map[string]any{"q": "kettles", "sort": "title", "limit": 1, "after": *page.Next})
	last := wantSearch(t, env)
	if got := last.ids(); len(got) != 1 || got[0] != "I-1" || last.Next != nil {
		t.Fatalf("page 2 = %v next=%v", got, last.Next)
	}
	if text != "1 products on this page (last page)" {
		t.Fatalf("summary = %q", text)
	}
}

func assertFirstPage(t *testing.T, skill string, page *wireSearch) {
	t.Helper()
	if skill != skillSearchProducts || len(page.Hits) != 1 || page.Hits[0].CatalogItemID != "I-2" {
		t.Fatalf("page 1 = %+v", page)
	}
	if h := page.Hits[0]; h.EffectivePrice == nil || h.EffectivePrice.AmountMinor != 8000 || len(h.CategoryPath) != 2 || h.CategoryPath[1] != "Kettles" {
		t.Fatalf("hit = %+v", h)
	}
	if len(page.Facets) != 3 || len(page.CategorySuggestions) != 1 || page.CategorySuggestions[0].CategoryID != "kettles" {
		t.Fatalf("facets/suggestions = %+v", page)
	}
	if page.Next == nil || *page.Next == "" {
		t.Fatalf("next = %v, want a cursor", page.Next)
	}
}

func TestWireSearchProductsAppliesTheFilters(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	cases := map[string]struct {
		args map[string]any
		want []string
	}{
		"no arguments browse every purchasable product": {map[string]any{"sort": "price-asc"}, []string{"I-1", "I-2"}},
		"priceMax":               {map[string]any{"priceMax": 5000}, []string{"I-1"}},
		"priceMin":               {map[string]any{"priceMin": 5000, "sort": "price-desc"}, []string{"I-2"}},
		"category subtree":       {map[string]any{"categoryId": "home", "sort": "title"}, []string{"I-2", "I-1"}},
		"inStock true":           {map[string]any{"inStock": true, "sort": "title"}, []string{"I-2", "I-1"}},
		"inStock false":          {map[string]any{"inStock": false}, nil},
		"unknown category":       {map[string]any{"categoryId": "nope"}, nil},
		"typo falls back to trg": {map[string]any{"q": "ketle", "sort": "title"}, []string{"I-2", "I-1"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, _ := w.skill(t, skillSearchProducts, tc.args)
			got := wantSearch(t, env).ids()
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

// A stub (an item the item master has not announced) is never listed.
func TestWireSearchNeverListsAStubAndAnEmptyAnswerHasEmptyArrays(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	env, _ := w.skill(t, skillSearchProducts, map[string]any{"priceMax": 1000})
	if got := wantSearch(t, env).ids(); len(got) != 0 {
		t.Fatalf("the stub S-1 (900) leaked into the search: %v", got)
	}

	empty := newWire(t, newFixture(t).deps())
	reply := empty.send(t, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": skillSearchProducts, "arguments": map[string]any{"q": "zzzzzz"}})))
	raw, _ := json.Marshal(reply.Parts[0].Content.(a2a.Data).Value)
	if !strings.Contains(string(raw), `"hits":[]`) || !strings.Contains(string(raw), `"next":null`) {
		t.Fatalf("an empty page must carry hits [] and next null: %s", raw)
	}
}

func TestWireGetProductReturnsThePageView(t *testing.T) {
	w := newWire(t, newFixture(t).deps())

	env, text := w.skill(t, skillGetProduct, map[string]any{"catalogItemId": "I-1"})
	if env.Problem != nil || env.Data == nil || env.Data.Product == nil {
		t.Fatalf("envelope = %+v", env)
	}
	unit := "l"
	want := wireProduct{
		CatalogItemID: "I-1", Title: "Steel Kettle", Hazmat: true, Description: "Boils water", Bullets: []string{"Fast"},
		CategoryID: "kettles", CategoryPath: []string{"Home", "Kettles"},
		ListPrice: &wireMoney{AmountMinor: 3500, Currency: "USD"}, EffectivePrice: &wireMoney{AmountMinor: 3000, Currency: "USD"},
		InStock: true, Purchasable: true,
		Attributes: []wireAttribute{{Name: "capacity", Value: "1.7", Unit: &unit}, {Name: "colour", Value: "red"}},
		Media:      []wireMedia{{MediaID: "m1", URL: "https://cdn.example.test/m1.jpg", AltText: "alt", Role: "PRIMARY"}},
	}
	if !reflect.DeepEqual(*env.Data.Product, want) {
		t.Fatalf("product = %+v\nwant      %+v", *env.Data.Product, want)
	}
	if text != `I-1: "Steel Kettle", purchasable, effective price 3000 USD (minor units)` {
		t.Fatalf("summary = %q", text)
	}
}

// An unknown id, a stub and an identifier that cannot name an item are all the
// 404 the REST adapter answers for GET /products/{catalogItemId}.
func TestWireGetProductNotFoundIsAnsweredAs404NotRaised(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	for name, id := range map[string]string{
		"unknown":  "GHOST",
		"stub":     "S-1",
		"too long": strings.Repeat("x", 1000),
		"NUL":      "I-1\x00",
	} {
		t.Run(name, func(t *testing.T) {
			env, text := w.skill(t, skillGetProduct, map[string]any{"catalogItemId": id})
			wantProblem(t, env, 404, "not-found")
			for _, s := range []string{env.Problem.Detail, env.Problem.Title, text} {
				if strings.Contains(s, "usecases") || len(s) > 200 {
					t.Fatalf("an answer leaks internal text or echoes an over-long id: %q", s)
				}
			}
		})
	}
	env, _ := w.skill(t, skillGetProduct, map[string]any{"catalogItemId": "GHOST"})
	if env.Problem.Detail != `no product exists with catalogItemId "GHOST"` {
		t.Fatalf("detail = %q", env.Problem.Detail)
	}
}

// An identifier carrying a NUL never reaches the repository (the REST adapter's
// rejectNUL guard): on the real Postgres it would be a 500, here a 404.
func TestWireGetProductKeepsANULAwayFromTheRepository(t *testing.T) {
	deps := newFixture(t).deps()
	deps.Queries.Products = nulRejectingProducts{deps.Queries.Products}
	env, _ := newWire(t, deps).skill(t, skillGetProduct, map[string]any{"catalogItemId": "I-1\x00"})
	wantProblem(t, env, 404, "not-found")
}

// The same classification as the REST adapter: a bad value for a known
// argument is a 422 validation-failed naming the field.
func TestWireSearchValidationFailuresAre422(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	cases := map[string]struct {
		args  map[string]any
		field string
	}{
		"unknown sort":       {map[string]any{"sort": "cheapest"}, "sort"},
		"limit zero":         {map[string]any{"limit": 0}, "limit"},
		"limit too big":      {map[string]any{"limit": 101}, "limit"},
		"limit negative":     {map[string]any{"limit": -1}, "limit"},
		"limit a string":     {map[string]any{"limit": "5"}, "limit"},
		"limit fractional":   {map[string]any{"limit": 1.5}, "limit"},
		"inStock a string":   {map[string]any{"inStock": "yes"}, "inStock"},
		"priceMin a string":  {map[string]any{"priceMin": "5"}, "priceMin"},
		"priceMax fraction":  {map[string]any{"priceMax": 10.5}, "priceMax"},
		"priceMin negative":  {map[string]any{"priceMin": -1}, "priceMin"},
		"priceMin above max": {map[string]any{"priceMin": 9, "priceMax": 8}, "priceMin"},
		"q too long":         {map[string]any{"q": strings.Repeat("a", 201)}, "q"},
		"q a number":         {map[string]any{"q": 7}, "q"},
		"foreign cursor":     {map[string]any{"after": "not-a-cursor"}, "after"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, text := w.skill(t, skillSearchProducts, tc.args)
			wantProblem(t, env, 422, "validation-failed")
			if !strings.HasPrefix(env.Problem.Detail, tc.field+":") || !strings.Contains(text, "Validation failed (422)") {
				t.Fatalf("detail = %q, summary = %q, want the field %s named", env.Problem.Detail, text, tc.field)
			}
		})
	}
}

// A malformed argument object is a 400 malformed-request, like the REST
// adapter's answer for a NUL in the query string.
func TestWireArgumentShapeProblemsAre400(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	cases := map[string]struct {
		skill string
		args  map[string]any
	}{
		"get: missing catalogItemId":    {skillGetProduct, map[string]any{}},
		"get: empty catalogItemId":      {skillGetProduct, map[string]any{"catalogItemId": ""}},
		"get: catalogItemId wrong type": {skillGetProduct, map[string]any{"catalogItemId": 7}},
		"get: unknown argument":         {skillGetProduct, map[string]any{"catalogItemId": "I-1", "force": true}},
		"get: typo'd argument":          {skillGetProduct, map[string]any{"catalogItemID": "I-1", "catalogItemIdd": "I-1"}},
		"get: search argument":          {skillGetProduct, map[string]any{"catalogItemId": "I-1", "q": "kettle"}},
		"search: unknown argument":      {skillSearchProducts, map[string]any{"inStok": true}},
		"search: get argument":          {skillSearchProducts, map[string]any{"catalogItemId": "I-1"}},
		"search: NUL in q":              {skillSearchProducts, map[string]any{"q": "ket\x00tle"}},
		"search: NUL in categoryId":     {skillSearchProducts, map[string]any{"categoryId": "\x00"}},
		"search: NUL in after":          {skillSearchProducts, map[string]any{"after": "a\x00"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, _ := w.skill(t, tc.skill, tc.args)
			wantProblem(t, env, 400, "malformed-request")
		})
	}
}

func TestWireRequestShapeProblems(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	shapes := map[string]*a2a.Part{
		"not json at all":      a2a.NewTextPart("not json at all"),
		"no skill":             a2a.NewDataPart(map[string]any{"arguments": map[string]any{}}),
		"unknown envelope key": a2a.NewDataPart(map[string]any{"skill": skillSearchProducts, "arguments": map[string]any{}, "extra": 1}),
		"trailing data":        a2a.NewTextPart(`{"skill":"search_products","arguments":{}} {"skill":"get_product"}`),
		"skill not a string":   a2a.NewDataPart(map[string]any{"skill": 7}),
		"arguments not object": a2a.NewDataPart(map[string]any{"skill": skillGetProduct, "arguments": "I-1"}),
		"binary part":          a2a.NewRawPart([]byte("x")),
	}
	for name, part := range shapes {
		t.Run(name, func(t *testing.T) {
			env, text := w.call(t, part)
			wantProblem(t, env, 400, "malformed-request")
			if strings.Contains(text, "Go value") || strings.Contains(text, "a2a.") {
				t.Fatalf("an answer leaks a Go type name: %q", text)
			}
		})
	}

	// A text part holding the same JSON is accepted (documented contract).
	env, _ := w.call(t, a2a.NewTextPart(`{"skill":"get_product","arguments":{"catalogItemId":"I-1"}}`))
	if env.Problem != nil || env.Data == nil || env.Data.Product == nil || env.Data.Product.CatalogItemID != "I-1" {
		t.Fatalf("a text part holding the skill JSON must be accepted, got %+v", env)
	}
	// Omitted arguments are the empty object: search browses everything.
	env, _ = w.call(t, a2a.NewDataPart(map[string]any{"skill": skillSearchProducts}))
	if got := wantSearch(t, env).ids(); len(got) != 2 {
		t.Fatalf("search without arguments = %v", got)
	}
}

// The agent is read-only: no write skill exists, whatever it is called, and the
// store is left untouched.
func TestWireOffersNoWriteSkill(t *testing.T) {
	f := newFixture(t)
	before := f.store.Counts()
	w := newWire(t, f.deps())

	for _, skill := range []string{"launch_rocket", "update_product", "set_price", "publish_content", "create_product", "list_categories", "Search_Products", "search_products "} {
		env, text := w.skill(t, skill, map[string]any{"catalogItemId": "NEW", "title": "x"})
		wantProblem(t, env, 400, "malformed-request")
		if env.Skill != skill || !strings.Contains(text, "is not offered by this agent") {
			t.Fatalf("skill %q: envelope skill %q, summary %q", skill, env.Skill, text)
		}
	}
	if after := f.store.Counts(); after != before {
		t.Fatalf("a skill name wrote to the store: %+v -> %+v", before, after)
	}
}

// The reply carries the caller's contextId, so a delegating saga can group its
// interactions.
func TestWireReplyKeepsTheCallersContext(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": skillGetProduct, "arguments": map[string]any{"catalogItemId": "I-1"}}))
	msg.ContextID = "ctx-42"
	if reply := w.send(t, msg); reply.ContextID != "ctx-42" {
		t.Fatalf("reply contextId = %q, want ctx-42", reply.ContextID)
	}
}

// ---- unexpected failures ----------------------------------------------------

var errBoom = errors.New("pgx: connection refused to 10.0.0.7:5432 password=hunter2")

type failingProducts struct{ ports.ProductRepository }

func (failingProducts) Get(context.Context, string) (productview.View, error) {
	return productview.View{}, errBoom
}

// nulRejectingProducts behaves like PostgreSQL: text cannot hold U+0000, and a
// statement carrying one fails (SQLSTATE 22021), which would be a 500 unless the
// adapter keeps the identifier away from the repository.
type nulRejectingProducts struct{ ports.ProductRepository }

func (r nulRejectingProducts) Get(ctx context.Context, id string) (productview.View, error) {
	if strings.ContainsRune(id, 0) {
		return productview.View{}, errors.New("ERROR: invalid byte sequence for encoding \"UTF8\": 0x00 (SQLSTATE 22021)")
	}
	return r.ProductRepository.Get(ctx, id)
}

type failingSearch struct{}

func (failingSearch) Search(context.Context, readmodel.SearchRequest) (readmodel.SearchResult, error) {
	return readmodel.SearchResult{}, errBoom
}

// An unexpected failure is answered as a 500 problem, never a JSON-RPC error,
// and its raw text (which may carry infrastructure detail) is not echoed.
func TestWireUnexpectedFailureIs500AndDoesNotLeakTheCause(t *testing.T) {
	f := newFixture(t)
	deps := f.deps()
	deps.Queries.Products = failingProducts{deps.Queries.Products}
	deps.Queries.Search = failingSearch{}
	w := newWire(t, deps)

	cases := map[string]struct {
		skill string
		args  map[string]any
	}{
		"get":    {skillGetProduct, map[string]any{"catalogItemId": "I-1"}},
		"search": {skillSearchProducts, map[string]any{"q": "kettle"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, text := w.skill(t, tc.skill, tc.args)
			wantProblem(t, env, 500, "internal-error")
			for _, s := range []string{env.Problem.Detail, env.Problem.Title, text} {
				if strings.Contains(s, "hunter2") || strings.Contains(s, "10.0.0.7") {
					t.Fatalf("a 500 answer leaks the underlying error: %q", s)
				}
			}
		})
	}
}

// ---- tracing ---------------------------------------------------------------

func TestSkillRunsInAnA2ASkillSpan(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	w := newWire(t, newFixture(t).deps())

	w.skill(t, skillGetProduct, map[string]any{"catalogItemId": "GHOST"})
	w.skill(t, "launch_rocket", nil)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1 (an unknown skill name must not become a span)", len(spans))
	}
	span := spans[0]
	if span.Name() != "a2a.skill get_product" {
		t.Fatalf("span name = %q", span.Name())
	}
	if !hasAttr(span.Attributes(), "a2a.skill", "get_product") {
		t.Fatalf("attributes = %v, want a2a.skill=get_product", span.Attributes())
	}
	if span.Status().Code.String() != "Error" {
		t.Fatalf("a problem answer must mark the span as an error, got %v", span.Status())
	}
}

func hasAttr(attrs []attribute.KeyValue, key, value string) bool {
	for _, a := range attrs {
		if string(a.Key) == key && a.Value.AsString() == value {
			return true
		}
	}
	return false
}

// TestCardIsServedAndDialable proves discovery end to end: a client built from
// the card document fetched over HTTP reaches the agent.
func TestCardIsServedAndDialable(t *testing.T) {
	f := newFixture(t)
	mux := http.NewServeMux()
	mux.Handle("/", Handler(NewExecutor(f.deps())))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	card := Card(Config{AgentURL: srv.URL + "/", Version: "test"})
	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatal(err)
	}
	w := &wire{client: client}
	env, _ := w.skill(t, skillGetProduct, map[string]any{"catalogItemId": "I-2"})
	if env.Data == nil || env.Data.Product == nil || env.Data.Product.Title != "Glass Kettle" {
		t.Fatalf("a client built from the card could not call the agent: %+v", env)
	}
}
