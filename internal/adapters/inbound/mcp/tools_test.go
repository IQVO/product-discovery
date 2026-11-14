package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/IQVO/product-discovery/internal/adapters/inbound/mcp"
	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// wire* are TEST-LOCAL copies of the answer contract (the REST bodies). If the
// adapter's output drifts, this suite fails instead of silently following it.
type wireMoney struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
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

func decode[T any](t *testing.T, sc map[string]any) T {
	t.Helper()
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("structured content does not decode: %v (%s)", err, raw)
	}
	return out
}

type wireSearch struct {
	Items []struct {
		CatalogItemID  string     `json:"catalogItemId"`
		EffectivePrice *wireMoney `json:"effectivePrice"`
		CategoryPath   []string   `json:"categoryPath"`
	} `json:"items"`
	Facets              []struct{ Name string } `json:"facets"`
	CategorySuggestions []struct {
		CategoryID string `json:"categoryId"`
	} `json:"categorySuggestions"`
	NextCursor string `json:"nextCursor"`
}

func (s wireSearch) ids() []string {
	var out []string
	for _, h := range s.Items {
		out = append(out, h.CatalogItemID)
	}
	return out
}

// search_products pages like GET /products: items, facets, category
// suggestions for a product-type query and the opaque nextCursor, which is
// passed back as cursor with the same filters and sort.
func TestSearchProductsPagesLikeREST(t *testing.T) {
	h := newHarness(t)

	sc := h.ok("search_products", map[string]any{"q": "kettles", "sort": "title", "limit": 1})
	for _, retired := range []string{"hits", "next"} {
		if _, has := sc[retired]; has {
			t.Fatalf("the answer carries the retired %q key: %v", retired, sc)
		}
	}
	first := decode[wireSearch](t, sc)
	assertFirstPage(t, first)

	sc = h.ok("search_products", map[string]any{"q": "kettles", "sort": "title", "limit": 1, "cursor": first.NextCursor})
	last := decode[wireSearch](t, sc)
	if got := last.ids(); len(got) != 1 || got[0] != "I-1" {
		t.Fatalf("page 2 = %v, want [I-1]", got)
	}
	if _, has := sc["nextCursor"]; has {
		t.Fatalf("nextCursor must be omitted on the last page: %v", sc)
	}
}

func assertFirstPage(t *testing.T, first wireSearch) {
	t.Helper()
	if got := first.ids(); len(got) != 1 || got[0] != "I-2" {
		t.Fatalf("page 1 = %v, want [I-2]", got)
	}
	hit := first.Items[0]
	if hit.EffectivePrice == nil || hit.EffectivePrice.AmountMinor != 8000 || len(hit.CategoryPath) != 2 || hit.CategoryPath[1] != "Kettles" {
		t.Fatalf("hit = %+v", hit)
	}
	if len(first.Facets) != 3 || len(first.CategorySuggestions) != 1 || first.CategorySuggestions[0].CategoryID != "kettles" {
		t.Fatalf("facets/suggestions = %+v", first)
	}
	if first.NextCursor == "" {
		t.Fatal("nextCursor is empty on a page with more rows")
	}
}

func TestSearchProductsAppliesTheFilters(t *testing.T) {
	h := newHarness(t)
	cases := map[string]struct {
		args map[string]any
		want []string
	}{
		"no arguments browse every purchasable product": {map[string]any{"sort": "price-asc"}, []string{"I-1", "I-2"}},
		"priceMax":               {map[string]any{"priceMax": 5000}, []string{"I-1"}},
		"priceMin":               {map[string]any{"priceMin": 5000, "sort": "price-desc"}, []string{"I-2"}},
		"priceMin zero":          {map[string]any{"priceMin": 0, "sort": "title"}, []string{"I-2", "I-1"}},
		"category subtree":       {map[string]any{"categoryId": "home", "sort": "title"}, []string{"I-2", "I-1"}},
		"inStock true":           {map[string]any{"inStock": true, "sort": "title"}, []string{"I-2", "I-1"}},
		"inStock false":          {map[string]any{"inStock": false}, nil},
		"unknown category":       {map[string]any{"categoryId": "nope"}, nil},
		"typo falls back to trg": {map[string]any{"q": "ketle", "sort": "title"}, []string{"I-2", "I-1"}},
		"limit 2":                {map[string]any{"limit": 2, "sort": "price-desc"}, []string{"I-2", "I-1"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := decode[wireSearch](t, h.ok("search_products", tc.args)).ids()
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("items = %v, want %v", got, tc.want)
			}
		})
	}
}

// A stub (an item the item master has not announced) is never listed, and an
// empty answer carries items [] and facets, never null.
func TestSearchProductsNeverListsAStubAndAnEmptyAnswerHasEmptyArrays(t *testing.T) {
	h := newHarness(t)
	if got := decode[wireSearch](t, h.ok("search_products", map[string]any{"priceMax": 1000})).ids(); len(got) != 0 {
		t.Fatalf("the stub S-1 (900) leaked into the search: %v", got)
	}
	sc := h.ok("search_products", map[string]any{"q": "zzzzzz"})
	if items, isList := sc["items"].([]any); !isList || len(items) != 0 {
		t.Fatalf("items = %#v, want an empty array", sc["items"])
	}
	if facets, isList := sc["facets"].([]any); !isList {
		t.Fatalf("facets = %#v, want an array", facets)
	}
}

// The REST classification of the errors a search can raise: validation-failed
// (422) names the field, invalid-limit and invalid-cursor are REST's 400s.
func TestSearchProductsErrorsCarryTheRESTSlug(t *testing.T) {
	h := newHarness(t)
	cases := map[string]struct {
		args map[string]any
		slug string
	}{
		"unknown sort":       {map[string]any{"sort": "cheapest"}, "validation-failed"},
		"priceMin negative":  {map[string]any{"priceMin": -1}, "validation-failed"},
		"priceMax negative":  {map[string]any{"priceMax": -1}, "validation-failed"},
		"priceMin above max": {map[string]any{"priceMin": 9, "priceMax": 8}, "validation-failed"},
		"q too long":         {map[string]any{"q": strings.Repeat("a", 201)}, "validation-failed"},
		"limit zero":         {map[string]any{"limit": 0}, "invalid-limit"},
		"limit too big":      {map[string]any{"limit": 501}, "invalid-limit"},
		"limit negative":     {map[string]any{"limit": -1}, "invalid-limit"},
		"foreign cursor":     {map[string]any{"cursor": "not-a-cursor"}, "invalid-cursor"},
		"NUL in q":           {map[string]any{"q": "ket\x00tle"}, "malformed-request"},
		"NUL in categoryId":  {map[string]any{"categoryId": "a\x00"}, "malformed-request"},
		"NUL in sort":        {map[string]any{"sort": "title\x00"}, "malformed-request"},
		"NUL in cursor":      {map[string]any{"cursor": "a\x00"}, "malformed-request"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h.failWith("search_products", tc.args, tc.slug)
		})
	}
}

// A value of the wrong JSON type never reaches a use case: the published input
// schema refuses it (a protocol error or an isError result, never a success).
func TestMistypedInputsAreRefused(t *testing.T) {
	h := newHarness(t)
	for name, tc := range map[string]struct {
		tool string
		args map[string]any
	}{
		"limit a string":      {"search_products", map[string]any{"limit": "5"}},
		"limit fractional":    {"search_products", map[string]any{"limit": 1.5}},
		"inStock a string":    {"search_products", map[string]any{"inStock": "yes"}},
		"priceMax fractional": {"search_products", map[string]any{"priceMax": 10.5}},
		"q a number":          {"search_products", map[string]any{"q": 7}},
		"unknown argument":    {"search_products", map[string]any{"inStok": true}},
		"id missing":          {"get_product", map[string]any{}},
		"id a number":         {"get_product", map[string]any{"catalogItemId": 7}},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err == nil && !res.IsError {
				t.Fatalf("%s %v was accepted: %#v", tc.tool, tc.args, res.StructuredContent)
			}
		})
	}
}

func TestGetProductReturnsThePageView(t *testing.T) {
	h := newHarness(t)
	got := decode[wireProduct](t, h.ok("get_product", map[string]any{"catalogItemId": "I-1"}))
	unit := "l"
	want := wireProduct{
		CatalogItemID: "I-1", Title: "Steel Kettle", Hazmat: true, Description: "Boils water", Bullets: []string{"Fast"},
		CategoryID: "kettles", CategoryPath: []string{"Home", "Kettles"},
		ListPrice: &wireMoney{AmountMinor: 3500, Currency: "USD"}, EffectivePrice: &wireMoney{AmountMinor: 3000, Currency: "USD"},
		InStock: true, Purchasable: true,
		Attributes: []wireAttribute{{Name: "capacity", Value: "1.7", Unit: &unit}, {Name: "colour", Value: "red"}},
		Media:      []wireMedia{{MediaID: "m1", URL: "https://cdn.example.test/m1.jpg", AltText: "alt", Role: "PRIMARY"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("product = %+v\nwant      %+v", got, want)
	}
}

// An unknown id, a stub and an identifier that cannot name an item are all the
// not-found REST answers for GET /products/{catalogItemId}; the answer never
// echoes an over-long id nor leaks internal text.
func TestGetProductNotFound(t *testing.T) {
	h := newHarness(t)
	for name, id := range map[string]string{
		"unknown":  "GHOST",
		"stub":     "S-1",
		"too long": strings.Repeat("x", 1000),
		"NUL":      "I-1\x00",
		"empty":    "",
	} {
		t.Run(name, func(t *testing.T) {
			h.failWith("get_product", map[string]any{"catalogItemId": id}, "not-found")
			if msg := text(h.call("get_product", map[string]any{"catalogItemId": id})); strings.Contains(msg, "usecases") || len(msg) > 200 {
				t.Fatalf("an answer leaks internal text or echoes an over-long id: %q", msg)
			}
		})
	}
	res := h.call("get_product", map[string]any{"catalogItemId": "GHOST"})
	if got := text(res); got != `not-found: no product exists with catalogItemId "GHOST"` {
		t.Fatalf("error text = %q", got)
	}
}

// nulRejectingProducts behaves like PostgreSQL: a NUL in a text parameter is an
// error (SQLSTATE 22021), so a NUL that reached the repository would be a 500.
type nulRejectingProducts struct{ ports.ProductRepository }

func (n nulRejectingProducts) Get(ctx context.Context, id string) (productview.View, error) {
	if strings.ContainsRune(id, 0) {
		return productview.View{}, errors.New("invalid byte sequence (SQLSTATE 22021)") // untyped: would map to internal-error
	}
	return n.ProductRepository.Get(ctx, id)
}

// failingProducts is a repository whose every read fails with an
// infrastructure error that must never reach the model.
type failingProducts struct{ ports.ProductRepository }

func (failingProducts) Get(context.Context, string) (productview.View, error) {
	return productview.View{}, errors.New("dial tcp 10.0.0.5:5432: connect: refused (dsn password=secret)")
}

// An identifier carrying a NUL never reaches the repository.
func TestGetProductKeepsANULAwayFromTheRepository(t *testing.T) {
	h := newHarness(t)
	reads := h.reads
	reads.Products = nulRejectingProducts{reads.Products}
	session := connectSession(t, inboundmcp.Deps{Reads: reads})
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_product", Arguments: map[string]any{"catalogItemId": "I-1\x00"}})
	if err != nil || !res.IsError || !strings.HasPrefix(text(res), "not-found: ") {
		t.Fatalf("NUL id = %v %v, want a not-found tool result", res, err)
	}
}

// Anything that is not a typed read error is internal-error, and its cause is
// logged, never echoed to the model.
func TestUnexpectedErrorsDoNotLeak(t *testing.T) {
	h := newHarness(t)
	reads := h.reads
	reads.Products = failingProducts{reads.Products}
	session := connectSession(t, inboundmcp.Deps{Reads: reads})
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_product", Arguments: map[string]any{"catalogItemId": "I-1"}})
	if err != nil || !res.IsError {
		t.Fatalf("a failing repository = %v %v, want an isError result", res, err)
	}
	if got := text(res); got != "internal-error: an unexpected internal error occurred" || strings.Contains(got, "dsn") {
		t.Fatalf("error text = %q", got)
	}
}
