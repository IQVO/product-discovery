package memory

import (
	"context"
	"sort"
	"strings"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// Field weights of the text rank: title A, attributes B, description C, plus
// the A-weight of a category-name match (ADR 0002 A4).
const (
	weightTitle    = 1.0
	weightAttr     = 0.4
	weightDesc     = 0.2
	weightCategory = 1.0
)

// Search is the in-memory ports.SearchPort. It applies the same filters,
// facets, ordering and keyset as the Postgres adapter, with the domain's
// token and trigram functions standing in for the full-text engine.
type Search struct{ s *Store }

type scored struct {
	v    productview.View
	rank float64
}

// Search implements ports.SearchPort.
func (e *Search) Search(ctx context.Context, req readmodel.SearchRequest) (readmodel.SearchResult, error) {
	defer e.s.enter(ctx)()
	var rows []scored
	for _, v := range e.s.products {
		if rank, ok := match(v, req); ok {
			rows = append(rows, scored{v, rank})
		}
	}
	sortRows(rows, req.Query.Sort)
	facets := tally(rows, req.Bands)
	rows = after(rows, req.Query)
	res := readmodel.SearchResult{Facets: facets}
	if len(rows) > req.Query.Limit {
		rows = rows[:req.Query.Limit]
		last := rows[len(rows)-1]
		res.Next = &search.Cursor{
			Sort: req.Query.Sort, Mode: req.Mode, Rank: last.rank, Price: last.v.EffectivePriceMinor,
			Title: strings.ToLower(last.v.Title), ID: last.v.CatalogItemID,
		}
	}
	for _, r := range rows {
		res.Hits = append(res.Hits, hit(r.v))
	}
	return res, nil
}

func match(v productview.View, req readmodel.SearchRequest) (float64, bool) {
	q := req.Query
	if !v.Searchable() || !filters(v, req) {
		return 0, false
	}
	switch {
	case req.Mode == search.ModeBrowse || q.Browse():
		return 0, true
	case req.Mode == search.ModeTrgm:
		r := search.BestSimilarity(v.Title, q.Text)
		return r, r >= search.SimilarityThreshold
	}
	rank := 0.0
	if search.CoversAll(v.Title, q.Text) {
		rank += weightTitle
	}
	if search.CoversAll(attributeText(v), q.Text) {
		rank += weightAttr
	}
	if search.CoversAll(v.Description+" "+strings.Join(v.Bullets, " "), q.Text) {
		rank += weightDesc
	}
	if contains(req.BoostCategoryIDs, v.CategoryID) {
		rank += weightCategory
	}
	return rank, rank > 0
}

func filters(v productview.View, req readmodel.SearchRequest) bool {
	q := req.Query
	switch {
	case req.CategoryIDs != nil && !contains(req.CategoryIDs, v.CategoryID):
		return false
	case q.InStock != nil && *q.InStock != v.InStock():
		return false
	case q.HasPriceFilter() && !v.HasPrice():
		return false
	case q.PriceMin != nil && v.EffectivePriceMinor < *q.PriceMin:
		return false
	case q.PriceMax != nil && v.EffectivePriceMinor > *q.PriceMax:
		return false
	case (q.Sort == search.SortPriceAsc || q.Sort == search.SortPriceDesc) && !v.HasPrice():
		return false
	}
	return true
}

func attributeText(v productview.View) string {
	var parts []string
	for _, a := range v.Attributes {
		parts = append(parts, a.Name, a.Value)
	}
	return strings.Join(parts, " ")
}

func contains(set []string, id string) bool {
	for _, s := range set {
		if s == id {
			return true
		}
	}
	return false
}

func hit(v productview.View) readmodel.Hit {
	h := readmodel.Hit{
		CatalogItemID: v.CatalogItemID, Title: v.Title, CategoryID: v.CategoryID, HasPrice: v.HasPrice(),
		InStock: v.InStock(), Purchasable: v.Purchasable(),
	}
	if v.HasPrice() {
		h.Currency, h.PriceMinor = v.Currency, v.EffectivePriceMinor
	}
	for i := range v.Media {
		if v.Media[i].Role == "PRIMARY" {
			m := v.Media[i]
			h.PrimaryMedia = &m
			break
		}
	}
	return h
}

func sortRows(rows []scored, by search.Sort) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch by {
		case search.SortPriceAsc:
			if a.v.EffectivePriceMinor != b.v.EffectivePriceMinor {
				return a.v.EffectivePriceMinor < b.v.EffectivePriceMinor
			}
		case search.SortPriceDesc:
			if a.v.EffectivePriceMinor != b.v.EffectivePriceMinor {
				return a.v.EffectivePriceMinor > b.v.EffectivePriceMinor
			}
		case search.SortTitle:
			if x, y := strings.ToLower(a.v.Title), strings.ToLower(b.v.Title); x != y {
				return x < y
			}
		default:
			if a.rank != b.rank {
				return a.rank > b.rank
			}
		}
		return a.v.CatalogItemID < b.v.CatalogItemID
	})
}

// after keeps the rows that sort strictly behind the cursor position (the
// keyset comparison of the SQL adapter, not a lookup of the cursor item).
func after(rows []scored, q search.Query) []scored {
	if q.After == nil {
		return rows
	}
	var out []scored
	for _, r := range rows {
		if behind(r, *q.After) {
			out = append(out, r)
		}
	}
	return out
}

func behind(r scored, c search.Cursor) bool {
	id := r.v.CatalogItemID
	switch c.Sort {
	case search.SortPriceAsc:
		return r.v.EffectivePriceMinor > c.Price || r.v.EffectivePriceMinor == c.Price && id > c.ID
	case search.SortPriceDesc:
		return r.v.EffectivePriceMinor < c.Price || r.v.EffectivePriceMinor == c.Price && id > c.ID
	case search.SortTitle:
		t := strings.ToLower(r.v.Title)
		return t > c.Title || t == c.Title && id > c.ID
	}
	return r.rank < c.Rank || r.rank == c.Rank && id > c.ID
}

func tally(rows []scored, bands []search.Band) []search.Facet {
	c := search.Counts{Category: map[string]int{}, Band: map[string]int{}}
	for _, r := range rows {
		if r.v.CategoryID != "" {
			c.Category[r.v.CategoryID]++
		}
		if r.v.HasPrice() {
			c.Band[search.BandOf(bands, r.v.EffectivePriceMinor).Label()]++
		}
		if r.v.InStock() {
			c.InStock++
		} else {
			c.OutStock++
		}
	}
	return search.NewFacets(c, bands)
}
