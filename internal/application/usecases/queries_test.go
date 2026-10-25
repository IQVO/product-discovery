package usecases_test

import (
	"errors"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

func seededEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv()
	e.publishTree(t, 1)
	e.makePurchasable(t, "I1", "Steel Kettle", "kettles")
	e.makePurchasable(t, "I2", "Garden Hose", "home")
	return e
}

func hitIDs(r usecases.SearchPage) []string {
	out := []string{}
	for _, h := range r.Hits {
		out = append(out, h.CatalogItemID)
	}
	return out
}

func TestSearchBrowseJoinsCategoryPaths(t *testing.T) {
	e := seededEnv(t)
	res, err := e.queries.SearchProducts(bg(), search.Input{Sort: "title"})
	must(t, err)
	if got := hitIDs(res); len(got) != 2 || got[0] != "I2" || got[1] != "I1" {
		t.Fatalf("hits = %v", got)
	}
	if p := res.Hits[1].CategoryPath; len(p) != 2 || p[0] != "Home" || p[1] != "Kettles" {
		t.Fatalf("path = %v", p)
	}
	if len(res.Suggestions) != 0 || res.Next != "" || len(res.Facets) != 3 {
		t.Fatalf("browse result = %+v", res)
	}
}

func TestSearchFiltersBySubtreeAndRejectsUnknownCategory(t *testing.T) {
	e := seededEnv(t)
	res, err := e.queries.SearchProducts(bg(), search.Input{CategoryID: "home", Sort: "title"})
	must(t, err)
	if len(res.Hits) != 2 {
		t.Fatalf("the home subtree holds both: %v", hitIDs(res))
	}
	res, err = e.queries.SearchProducts(bg(), search.Input{CategoryID: "kettles"})
	must(t, err)
	if got := hitIDs(res); len(got) != 1 || got[0] != "I1" {
		t.Fatalf("kettles = %v", got)
	}
	res, err = e.queries.SearchProducts(bg(), search.Input{CategoryID: "nope"})
	must(t, err)
	if len(res.Hits) != 0 {
		t.Fatalf("unknown category matches nothing: %v", hitIDs(res))
	}
}

func TestSearchTextSuggestsCategoriesAndBoosts(t *testing.T) {
	e := seededEnv(t)
	res, err := e.queries.SearchProducts(bg(), search.Input{Text: "kettles"})
	must(t, err)
	if got := hitIDs(res); len(got) != 1 || got[0] != "I1" {
		t.Fatalf("hits = %v", got)
	}
	if len(res.Suggestions) != 1 || res.Suggestions[0].ID != "kettles" {
		t.Fatalf("suggestions = %+v", res.Suggestions)
	}
}

func TestSearchFallsBackToTrigramOnZeroFullTextRows(t *testing.T) {
	e := seededEnv(t)
	res, err := e.queries.SearchProducts(bg(), search.Input{Text: "ketle"})
	must(t, err)
	if got := hitIDs(res); len(got) != 1 || got[0] != "I1" {
		t.Fatalf("a typo finds the kettle through pg_trgm, got %v", got)
	}
	res, err = e.queries.SearchProducts(bg(), search.Input{Text: "zeppelin"})
	must(t, err)
	if len(res.Hits) != 0 {
		t.Fatalf("nothing similar: %v", hitIDs(res))
	}
}

func TestSearchPagesAndRejectsBadInput(t *testing.T) {
	e := seededEnv(t)
	first, err := e.queries.SearchProducts(bg(), search.Input{Sort: "title", Limit: 1})
	must(t, err)
	if first.Next == "" || len(first.Hits) != 1 {
		t.Fatalf("first page = %+v", first)
	}
	second, err := e.queries.SearchProducts(bg(), search.Input{Sort: "title", Limit: 1, After: first.Next})
	must(t, err)
	if second.Next != "" || second.Hits[0].CatalogItemID == first.Hits[0].CatalogItemID {
		t.Fatalf("second page = %+v", second)
	}
	if _, err := e.queries.SearchProducts(bg(), search.Input{Sort: "cheapest"}); !errors.Is(err, search.ErrValidation) {
		t.Fatalf("bad sort: %v", err)
	}
}

func TestListCategoriesRollsUpPurchasableCounts(t *testing.T) {
	e := seededEnv(t)
	got, err := e.queries.ListCategories(bg())
	must(t, err)
	counts := map[string]int{}
	for _, c := range got.Categories {
		counts[c.ID] = c.PurchasableCount
	}
	if got.Version != 1 || counts["home"] != 2 || counts["kettles"] != 1 {
		t.Fatalf("categories = %+v", got)
	}
	empty, err := newEnv().queries.ListCategories(bg())
	if err != nil || empty.Version != 0 || len(empty.Categories) != 0 {
		t.Fatalf("an unbuilt tree answers empty: %+v %v", empty, err)
	}
}
