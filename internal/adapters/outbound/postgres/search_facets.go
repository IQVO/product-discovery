package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// facets tallies category, price band and stock over the whole matched set in
// one GROUPING SETS query.
func (s *Search) facets(ctx context.Context, req readmodel.SearchRequest) ([]search.Facet, error) {
	var args sqlArgs
	matched := matchedCTE(req, &args)
	band := bandSQL(req.Bands)
	rows, err := s.pool.Query(ctx, matched+`, b AS (SELECT category_id, in_stock, has_price, `+band+` AS band FROM m)
		SELECT GROUPING(category_id, band, in_stock), category_id, band, in_stock, count(*)
		FROM b GROUP BY GROUPING SETS ((category_id), (band), (in_stock))`, args.vals...)
	if err != nil {
		return nil, fmt.Errorf("facets: %w", err)
	}
	defer rows.Close()
	c := search.Counts{Category: map[string]int{}, Band: map[string]int{}}
	for rows.Next() {
		var grouping, n int
		var bandIdx *int
		var cat *string
		var stock *bool
		if err := rows.Scan(&grouping, &cat, &bandIdx, &stock, &n); err != nil {
			return nil, fmt.Errorf("scan facet: %w", err)
		}
		switch grouping {
		case 3: // grouped by category_id only (band and in_stock rolled up)
			if cat != nil && *cat != "" {
				c.Category[*cat] += n
			}
		case 5: // grouped by band only
			if bandIdx != nil {
				c.Band[req.Bands[*bandIdx].Label()] += n
			}
		case 6: // grouped by in_stock only
			if stock != nil && *stock {
				c.InStock += n
			} else if stock != nil {
				c.OutStock += n
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return search.NewFacets(c, req.Bands), nil
}

// bandSQL is the CASE expression mapping a priced row to the index of its
// band, NULL for a row without a price.
func bandSQL(bands []search.Band) string {
	var b strings.Builder
	b.WriteString("CASE WHEN NOT has_price THEN NULL")
	for i, band := range bands {
		if band.Upper == 0 {
			fmt.Fprintf(&b, " ELSE %d", i)
			break
		}
		fmt.Fprintf(&b, " WHEN effective_price_minor < %d THEN %d", band.Upper, i)
	}
	b.WriteString(" END")
	return b.String()
}
