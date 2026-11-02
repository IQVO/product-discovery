package http_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/usecases"
)

func (e *env) retireAndWithdraw(id string) {
	e.t.Helper()
	bg := context.Background()
	e.must(e.consumer.ApplyProductContentRetired(bg, e.meta(), usecases.ProductContentRetired{CatalogItemID: id, Revision: 1}))
	e.must(e.consumer.ApplyItemPriceWithdrawn(bg, e.meta(), usecases.ItemPriceWithdrawn{CatalogItemID: id, Version: 2}))
}

func TestSearchReturnsHitsFacetsSuggestionsAndCursor(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	e.item("I2", "Glass Kettle", 8000)
	r := e.get("/products?q=kettles&sort=title&limit=1")
	e.wantStatus(r, http.StatusOK)
	m := r.json(t)
	hits := m["items"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["catalogItemId"] != "I2" {
		t.Fatalf("hits = %s", r.Body)
	}
	hit := hits[0].(map[string]any)
	if hit["effectivePrice"].(map[string]any)["amountMinor"] != float64(8000) || hit["primaryMedia"].(map[string]any)["mediaId"] != "m1" ||
		hit["categoryPath"].([]any)[1] != "Kettles" {
		t.Fatalf("hit = %v", hit)
	}
	if len(m["facets"].([]any)) != 3 {
		t.Fatalf("facets = %v", m["facets"])
	}
	if s := m["categorySuggestions"].([]any); len(s) != 1 || s[0].(map[string]any)["categoryId"] != "kettles" {
		t.Fatalf("suggestions = %v", s)
	}
	next, ok := m["nextCursor"].(string)
	if !ok || next == "" {
		t.Fatalf("nextCursor = %v", m["nextCursor"])
	}
	second := e.get("/products?q=kettles&sort=title&limit=1&cursor=" + next).json(t)
	if _, more := second["nextCursor"]; second["items"].([]any)[0].(map[string]any)["catalogItemId"] != "I1" || more {
		t.Fatalf("second page = %v", second)
	}
}

func TestSearchEmptyAnswersEmptyArrays(t *testing.T) {
	e := newEnv(t)
	r := e.get("/products")
	e.wantStatus(r, http.StatusOK)
	m := r.json(t)
	if len(m["items"].([]any)) != 0 || m["nextCursor"] != nil {
		t.Fatalf("empty = %s", r.Body)
	}
	for _, f := range m["facets"].([]any) {
		if f.(map[string]any)["values"] == nil {
			t.Fatalf("facet values must be [] not null: %s", r.Body)
		}
	}
}

func TestSearchFiltersParse(t *testing.T) {
	e := newEnv(t)
	e.item("I1", "Steel Kettle", 3000)
	e.item("I2", "Glass Kettle", 8000)
	for path, want := range map[string]int{
		"/products?inStock=true": 2, "/products?inStock=false": 0, "/products?priceMin=5000": 1,
		"/products?priceMax=5000": 1, "/products?categoryId=kettles": 0, "/products?priceMin=1&priceMax=9000&sort=price-desc": 2,
	} {
		if got := len(e.get(path).json(t)["items"].([]any)); got != want {
			t.Errorf("%s: %d hits, want %d", path, got, want)
		}
	}
}

func TestSearchRejectsBadParameters(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{
		"/products?sort=cheapest", "/products?inStock=maybe", "/products?inStock=1", "/products?priceMin=x",
		"/products?priceMax=1.5", "/products?priceMin=-1", "/products?priceMin=9&priceMax=1",
	} {
		e.wantProblem(e.get(path), http.StatusUnprocessableEntity, "validation-failed")
	}
}

func TestSearchPagingProblemsAreBadRequests(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{
		"/products?limit=0", "/products?limit=abc", "/products?limit=501", "/products?limit=-1", "/products?limit=",
		"/products?limit=1&limit=2",
	} {
		e.wantProblem(e.get(path), http.StatusBadRequest, "invalid-limit")
	}
	for _, path := range []string{"/products?cursor=junk", "/products?cursor=a&cursor=b"} {
		e.wantProblem(e.get(path), http.StatusBadRequest, "invalid-cursor")
	}
	for _, path := range []string{"/products?after=x", "/products?offset=1", "/products?nextToken=x", "/products?bogus=1"} {
		e.wantProblem(e.get(path), http.StatusBadRequest, "unknown-query-parameter")
	}
}

// TestSearchWalksEveryPageOnce pages rows that share a sort key (equal price) with
// limit 2 and asserts no duplicate and no skipped row.
func TestSearchWalksEveryPageOnce(t *testing.T) {
	e := newEnv(t)
	e.tree()
	want := map[string]bool{}
	for _, id := range []string{"I1", "I2", "I3", "I4", "I5"} {
		e.item(id, "Kettle "+id, 3000)
		want[id] = true
	}
	for _, sortBy := range []string{"price-asc", "price-desc", "title"} {
		seen := map[string]bool{}
		path := "/products?sort=" + sortBy + "&limit=2"
		for pages := 0; path != ""; pages++ {
			if pages > 10 {
				t.Fatalf("%s: cursor never ended", sortBy)
			}
			m := e.get(path).json(t)
			for _, h := range m["items"].([]any) {
				id := h.(map[string]any)["catalogItemId"].(string)
				if seen[id] {
					t.Fatalf("%s: %s returned twice", sortBy, id)
				}
				seen[id] = true
			}
			path = ""
			if next, ok := m["nextCursor"].(string); ok {
				path = "/products?sort=" + sortBy + "&limit=2&cursor=" + next
			}
		}
		if len(seen) != len(want) {
			t.Fatalf("%s: saw %v, want %v", sortBy, seen, want)
		}
	}
}

func TestHealthReadinessAndNUL(t *testing.T) {
	e := newEnv(t)
	e.wantStatus(e.get("/healthz"), http.StatusOK)
	e.wantStatus(e.get("/readyz"), http.StatusOK)
	e.ready.SetNotReady()
	e.wantStatus(e.get("/readyz"), http.StatusServiceUnavailable)
	e.wantStatus(e.get("/healthz"), http.StatusOK)
	e.wantProblem(e.get("/products/a%00b"), http.StatusNotFound, "resource-not-found")
	e.wantProblem(e.get("/products?q=a%00b"), http.StatusBadRequest, "malformed-request")
}
