package repocontract

import (
	"reflect"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// seedCatalogue stores the fixed catalogue every search test reads.
func seedCatalogue(t *testing.T, e Env) {
	t.Helper()
	saveAll(t, e,
		spec{id: "k1", title: "Steel Kettle 1.7L", desc: "Boils water quickly", category: "kettles", price: 3000, qty: 5,
			attrs: []productview.Attribute{{Name: "colour", Value: "silver"}}},
		spec{id: "k2", title: "Glass Kettle", desc: "See through body", category: "kettles", price: 8000},
		spec{id: "k3", title: "Electric Teapot", desc: "A kettle alternative for tea", category: "kitchen", price: 1500, qty: 2},
		spec{id: "g1", title: "Garden Hose", desc: "Twenty metres", category: "garden", price: 12000, qty: 3},
		spec{id: "g2", title: "Garden Rake", desc: "Steel tines", category: "garden", price: 30000, qty: 1},
		spec{id: "n1", title: "Mystery Box", desc: "Surprise", qty: 1},
		spec{id: "x1", title: "Old Kettle", desc: "Gone", category: "kettles", price: 100, qty: 1, discontinued: true},
		spec{id: "x2", title: "Hidden Kettle", category: "kettles", price: 100, qty: 1, noContent: true},
	)
	stub, _ := productview.Stub("x3")
	must(t, e.Products.Save(bg(), stub))
}

// query builds a validated Query from the input.
func query(t *testing.T, in search.Input) search.Query {
	t.Helper()
	q, err := search.NewQuery(in)
	must(t, err)
	return q
}

// request is the SearchRequest the use case would build for q.
func request(q search.Query) readmodel.SearchRequest {
	req := readmodel.SearchRequest{Query: q, Mode: search.ModeBrowse, Bands: search.Bands(search.DefaultBandEdges)}
	if !q.Browse() {
		req.Mode = search.ModeFTS
	}
	if q.After != nil {
		req.Mode = q.After.Mode
	}
	return req
}

func run(t *testing.T, e Env, req readmodel.SearchRequest) readmodel.SearchResult {
	t.Helper()
	res, err := e.Search.Search(bg(), req)
	must(t, err)
	return res
}

func ids(res readmodel.SearchResult) []string {
	out := []string{}
	for _, h := range res.Hits {
		out = append(out, h.CatalogItemID)
	}
	return out
}

func equalIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func testSearch(t *testing.T, e Env) {
	seedCatalogue(t, e)
	for name, fn := range map[string]func(*testing.T, Env){
		"browse":   searchBrowse,
		"paging":   searchPaging,
		"filters":  searchFilters,
		"text":     searchText,
		"facets":   searchFacets,
		"hit-data": searchHitData,
	} {
		t.Run(name, func(t *testing.T) { fn(t, e) })
	}
}
