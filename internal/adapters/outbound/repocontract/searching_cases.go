package repocontract

import (
	"sort"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

func sorted(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func searchBrowse(t *testing.T, e Env) {
	res := run(t, e, request(query(t, search.Input{Sort: "title"})))
	equalIDs(t, "browse by title", ids(res), []string{"k3", "g1", "g2", "k2", "n1", "k1"})
	if res.Next != nil {
		t.Errorf("one page holds everything, Next = %+v", res.Next)
	}
	res = run(t, e, request(query(t, search.Input{Sort: "price-asc"})))
	equalIDs(t, "price-asc skips unpriced rows", ids(res), []string{"k3", "k1", "k2", "g1", "g2"})
	res = run(t, e, request(query(t, search.Input{Sort: "price-desc"})))
	equalIDs(t, "price-desc", ids(res), []string{"g2", "g1", "k2", "k1", "k3"})
}

func searchPaging(t *testing.T, e Env) {
	for _, sortBy := range []string{"title", "price-asc", "price-desc", "relevance"} {
		full := ids(run(t, e, request(query(t, search.Input{Sort: sortBy}))))
		var got []string
		after := ""
		for page := 0; page < 10; page++ {
			res := run(t, e, request(query(t, search.Input{Sort: sortBy, Limit: 2, After: after})))
			if len(res.Hits) > 2 {
				t.Fatalf("%s: page of %d hits with limit 2", sortBy, len(res.Hits))
			}
			got = append(got, ids(res)...)
			if res.Next == nil {
				break
			}
			if res.Next.Mode != search.ModeBrowse || res.Next.Sort != search.Sort(sortBy) {
				t.Fatalf("%s: cursor = %+v", sortBy, res.Next)
			}
			after = res.Next.Encode()
		}
		equalIDs(t, sortBy+" pages concatenated", got, full)
	}
}

func searchFilters(t *testing.T, e Env) {
	yes, no := true, false
	lo, hi := int64(2000), int64(9000)
	cases := []struct {
		name string
		req  readmodel.SearchRequest
		want []string
	}{
		{"in stock", request(query(t, search.Input{InStock: &yes, Sort: "title"})), []string{"k3", "g1", "g2", "n1", "k1"}},
		{"out of stock", request(query(t, search.Input{InStock: &no})), []string{"k2"}},
		{"price range", request(query(t, search.Input{PriceMin: &lo, PriceMax: &hi, Sort: "price-asc"})), []string{"k1", "k2"}},
		{"price floor only", request(query(t, search.Input{PriceMin: &hi, Sort: "price-asc"})), []string{"g1", "g2"}},
	}
	sub := request(query(t, search.Input{Sort: "title"}))
	sub.CategoryIDs = []string{"kitchen", "kettles"}
	cases = append(cases, struct {
		name string
		req  readmodel.SearchRequest
		want []string
	}{"category subtree", sub, []string{"k3", "k2", "k1"}})
	none := request(query(t, search.Input{}))
	none.CategoryIDs = []string{}
	cases = append(cases, struct {
		name string
		req  readmodel.SearchRequest
		want []string
	}{"unknown category", none, []string{}})
	for _, c := range cases {
		equalIDs(t, c.name, ids(run(t, e, c.req)), c.want)
	}
}

func searchText(t *testing.T, e Env) {
	res := run(t, e, request(query(t, search.Input{Text: "kettle"})))
	got := ids(res)
	if len(got) != 3 || got[2] != "k3" || (got[0] != "k1" && got[0] != "k2") {
		t.Errorf("kettle: title matches rank above the description match, got %v", got)
	}
	equalIDs(t, "attribute match", ids(run(t, e, request(query(t, search.Input{Text: "silver"})))), []string{"k1"})
	equalIDs(t, "no match", ids(run(t, e, request(query(t, search.Input{Text: "zeppelin"})))), []string{})
	equalIDs(t, "all words must match", ids(run(t, e, request(query(t, search.Input{Text: "kettle garden"})))), []string{})
	boost := request(query(t, search.Input{Text: "cookware", Sort: "title"}))
	boost.BoostCategoryIDs = []string{"garden"}
	equalIDs(t, "category-name match", ids(run(t, e, boost)), []string{"g1", "g2"})
	typo := request(query(t, search.Input{Text: "ketle"}))
	equalIDs(t, "full-text finds no typo", ids(run(t, e, typo)), []string{})
	typo.Mode = search.ModeTrgm
	res = run(t, e, typo)
	equalIDs(t, "trigram finds the typo", sorted(ids(res)), []string{"k1", "k2"})
	if res.Next != nil {
		t.Errorf("Next = %+v", res.Next)
	}
}

func searchFacets(t *testing.T, e Env) {
	res := run(t, e, request(query(t, search.Input{Limit: 2})))
	if len(res.Hits) != 2 || res.Next == nil {
		t.Fatalf("limit 2 must page: %d hits", len(res.Hits))
	}
	want := search.NewFacets(search.Counts{
		Category: map[string]int{"kettles": 2, "kitchen": 1, "garden": 2},
		Band:     map[string]int{"<2500": 1, "2500-4999": 1, "5000-9999": 1, "10000-24999": 1, "25000+": 1},
		InStock:  5, OutStock: 1,
	}, search.Bands(search.DefaultBandEdges))
	if len(res.Facets) != 3 {
		t.Fatalf("facets = %+v", res.Facets)
	}
	for i := range want {
		if want[i].Name != res.Facets[i].Name || !sameValues(want[i].Values, res.Facets[i].Values) {
			t.Errorf("facet %s = %+v, want %+v", want[i].Name, res.Facets[i].Values, want[i].Values)
		}
	}
}

func sameValues(a, b []search.FacetValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func searchHitData(t *testing.T, e Env) {
	res := run(t, e, request(query(t, search.Input{Text: "silver"})))
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %+v", res.Hits)
	}
	h := res.Hits[0]
	if h.Title != "Steel Kettle 1.7L" || h.CategoryID != "kettles" || !h.HasPrice || h.Currency != "USD" || h.PriceMinor != 3000 ||
		!h.InStock || !h.Purchasable || h.PrimaryMedia == nil || h.PrimaryMedia.MediaID != "m-k1" {
		t.Errorf("hit = %+v", h)
	}
	n := run(t, e, request(query(t, search.Input{Text: "mystery"}))).Hits[0]
	if n.HasPrice || n.Purchasable || !n.InStock || n.CategoryID != "" {
		t.Errorf("unpriced hit = %+v", n)
	}
}
