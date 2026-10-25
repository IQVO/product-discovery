package search

import (
	"cmp"
	"slices"
)

// The three facet names of a search result.
const (
	FacetCategory  = "category"
	FacetPriceBand = "price-band"
	FacetInStock   = "in-stock"
)

// FacetValue is one bucket of a facet.
type FacetValue struct {
	Value string
	Count int
}

// Facet is one named facet with its non-empty buckets.
type Facet struct {
	Name   string
	Values []FacetValue
}

// Counts is what an engine tallies over the rows a query matched, before any
// paging: items per category id, per price-band label and per stock state.
type Counts struct {
	Category map[string]int
	Band     map[string]int
	InStock  int
	OutStock int
}

// NewFacets turns the tallies into the three facets, in the order category,
// price-band, in-stock. Empty buckets are dropped; categories sort by count
// then id, bands in ascending price order, stock as true then false.
func NewFacets(c Counts, bands []Band) []Facet {
	cat := Facet{Name: FacetCategory}
	for id, n := range c.Category {
		if n > 0 {
			cat.Values = append(cat.Values, FacetValue{Value: id, Count: n})
		}
	}
	slices.SortFunc(cat.Values, func(a, b FacetValue) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Value, b.Value))
	})
	band := Facet{Name: FacetPriceBand}
	for _, b := range bands {
		if n := c.Band[b.Label()]; n > 0 {
			band.Values = append(band.Values, FacetValue{Value: b.Label(), Count: n})
		}
	}
	stock := Facet{Name: FacetInStock}
	if c.InStock > 0 {
		stock.Values = append(stock.Values, FacetValue{Value: "true", Count: c.InStock})
	}
	if c.OutStock > 0 {
		stock.Values = append(stock.Values, FacetValue{Value: "false", Count: c.OutStock})
	}
	return []Facet{cat, band, stock}
}
