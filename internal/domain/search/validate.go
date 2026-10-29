package search

import "fmt"

func parseSort(s string) (Sort, error) {
	switch Sort(s) {
	case "":
		return SortRelevance, nil
	case SortRelevance, SortPriceAsc, SortPriceDesc, SortTitle:
		return Sort(s), nil
	}
	return "", invalid("sort", "one of relevance, price-asc, price-desc, title")
}

func checkLimit(q *Query) error {
	if q.Limit == 0 {
		q.Limit = DefaultLimit
		return nil
	}
	if q.Limit < 1 || q.Limit > MaxLimit {
		return invalid("limit", fmt.Sprintf("between 1 and %d", MaxLimit))
	}
	return nil
}

func checkPrices(q Query) error {
	if q.PriceMin != nil && *q.PriceMin < 0 {
		return invalid("priceMin", "must not be negative")
	}
	if q.PriceMax != nil && *q.PriceMax < 0 {
		return invalid("priceMax", "must not be negative")
	}
	if q.PriceMin != nil && q.PriceMax != nil && *q.PriceMin > *q.PriceMax {
		return invalid("priceMin", "must not exceed priceMax")
	}
	return nil
}

// Browse reports whether the query has no free text: a plain filtered listing.
func (q Query) Browse() bool { return q.Text == "" }

// HasPriceFilter reports whether a price bound is set.
func (q Query) HasPriceFilter() bool { return q.PriceMin != nil || q.PriceMax != nil }
