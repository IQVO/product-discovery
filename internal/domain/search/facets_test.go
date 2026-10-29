package search_test

import (
	"reflect"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/search"
)

func TestNewFacetsOrdersAndDropsEmptyBuckets(t *testing.T) {
	f := search.NewFacets(search.Counts{
		Category: map[string]int{"b": 2, "a": 2, "c": 5, "z": 0},
		Band:     map[string]int{"25000+": 1, "<2500": 3, "5000-9999": 0},
		InStock:  4, OutStock: 1,
	}, search.Bands(search.DefaultBandEdges))
	if len(f) != 3 || f[0].Name != "category" || f[1].Name != "price-band" || f[2].Name != "in-stock" {
		t.Fatalf("facet order = %+v", f)
	}
	wantCat := []search.FacetValue{{Value: "c", Count: 5}, {Value: "a", Count: 2}, {Value: "b", Count: 2}}
	if !reflect.DeepEqual(f[0].Values, wantCat) {
		t.Errorf("category = %+v, want %+v", f[0].Values, wantCat)
	}
	wantBand := []search.FacetValue{{Value: "<2500", Count: 3}, {Value: "25000+", Count: 1}}
	if !reflect.DeepEqual(f[1].Values, wantBand) {
		t.Errorf("band = %+v, want %+v", f[1].Values, wantBand)
	}
	wantStock := []search.FacetValue{{Value: "true", Count: 4}, {Value: "false", Count: 1}}
	if !reflect.DeepEqual(f[2].Values, wantStock) {
		t.Errorf("stock = %+v, want %+v", f[2].Values, wantStock)
	}
}

func TestNewFacetsEmptyKeepsAllThreeFacets(t *testing.T) {
	f := search.NewFacets(search.Counts{}, search.Bands(search.DefaultBandEdges))
	if len(f) != 3 {
		t.Fatalf("want 3 facets, got %d", len(f))
	}
	for _, x := range f {
		if len(x.Values) != 0 {
			t.Errorf("%s must be empty, got %+v", x.Name, x.Values)
		}
	}
	f = search.NewFacets(search.Counts{OutStock: 2}, nil)
	if len(f[2].Values) != 1 || f[2].Values[0].Value != "false" {
		t.Errorf("only out-of-stock = %+v", f[2].Values)
	}
}
