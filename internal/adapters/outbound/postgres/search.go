package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// Search is the Postgres ports.SearchPort: full-text search over the GIN
// indexed tsvector, pg_trgm similarity as the typo fallback, keyset paging and
// grouping-set facets, all in plain SQL over products.
type Search struct{ pool *pgxpool.Pool }

var _ ports.SearchPort = (*Search)(nil)

// NewSearch constructs a Search over pool.
func NewSearch(pool *pgxpool.Pool) *Search { return &Search{pool: pool} }

// sqlArgs numbers positional parameters.
type sqlArgs struct{ vals []any }

func (a *sqlArgs) add(v any) string {
	a.vals = append(a.vals, v)
	return fmt.Sprintf("$%d", len(a.vals))
}

// Search implements ports.SearchPort.
func (s *Search) Search(ctx context.Context, req readmodel.SearchRequest) (readmodel.SearchResult, error) {
	var args sqlArgs
	matched := matchedCTE(req, &args)
	q := req.Query
	sqlText := matched + ` SELECT ` + hitColumns + ` FROM m` + keysetWhere(req, &args) + orderBy(q.Sort) + ` LIMIT ` + args.add(q.Limit+1)
	rows, err := s.pool.Query(ctx, sqlText, args.vals...)
	if err != nil {
		return readmodel.SearchResult{}, fmt.Errorf("search products: %w", err)
	}
	hits, ranks, err := scanHits(rows)
	if err != nil {
		return readmodel.SearchResult{}, err
	}
	res := readmodel.SearchResult{}
	if len(hits) > q.Limit {
		hits, ranks = hits[:q.Limit], ranks[:q.Limit]
		last := hits[len(hits)-1]
		res.Next = &search.Cursor{Sort: q.Sort, Mode: req.Mode, Rank: ranks[len(ranks)-1], Price: last.PriceMinor, Title: strings.ToLower(last.Title), ID: last.CatalogItemID}
	}
	res.Hits = hits
	if res.Facets, err = s.facets(ctx, req); err != nil {
		return readmodel.SearchResult{}, err
	}
	return res, nil
}

const hitColumns = `catalog_item_id, title, category_id, has_price, currency, effective_price_minor, media, in_stock, purchasable, rank`

func scanHits(rows pgx.Rows) ([]readmodel.Hit, []float64, error) {
	defer rows.Close()
	var hits []readmodel.Hit
	var ranks []float64
	for rows.Next() {
		var h readmodel.Hit
		var media []byte
		var rank float64
		if err := rows.Scan(&h.CatalogItemID, &h.Title, &h.CategoryID, &h.HasPrice, &h.Currency, &h.PriceMinor, &media, &h.InStock, &h.Purchasable, &rank); err != nil {
			return nil, nil, fmt.Errorf("scan hit: %w", err)
		}
		var v productview.View
		if err := decodeColumns(&v, []byte(`[]`), []byte(`[]`), media, []byte(`[]`)); err != nil {
			return nil, nil, err
		}
		for i := range v.Media {
			if v.Media[i].Role == "PRIMARY" {
				m := v.Media[i]
				h.PrimaryMedia = &m
				break
			}
		}
		if !h.HasPrice {
			h.Currency, h.PriceMinor = "", 0
		}
		hits = append(hits, h)
		ranks = append(ranks, rank)
	}
	return hits, ranks, rows.Err()
}

// matchedCTE builds `WITH m AS (...)`: the rows the request matches, each with
// its rank, before the keyset position and the page size apply.
func matchedCTE(req readmodel.SearchRequest, args *sqlArgs) string {
	q := req.Query
	conds := []string{"searchable"}
	rank := "0::float8"
	if req.CategoryIDs != nil {
		conds = append(conds, "category_id = ANY("+args.add(req.CategoryIDs)+")")
	}
	if q.InStock != nil {
		conds = append(conds, "in_stock = "+args.add(*q.InStock))
	}
	if q.HasPriceFilter() || q.Sort == search.SortPriceAsc || q.Sort == search.SortPriceDesc {
		conds = append(conds, "has_price")
	}
	if q.PriceMin != nil {
		conds = append(conds, "effective_price_minor >= "+args.add(*q.PriceMin))
	}
	if q.PriceMax != nil {
		conds = append(conds, "effective_price_minor <= "+args.add(*q.PriceMax))
	}
	switch {
	case q.Browse() || req.Mode == search.ModeBrowse:
	case req.Mode == search.ModeTrgm:
		text := args.add(q.Text)
		rank = "GREATEST(similarity(title, " + text + "), word_similarity(" + text + ", title))::float8"
		conds = append(conds, rank+" >= "+fmt.Sprint(search.SimilarityThreshold))
	default:
		tsq := "plainto_tsquery('english', " + args.add(q.Text) + ")"
		boost := boostSQL(req.BoostCategoryIDs, args)
		rank = "(ts_rank_cd(doc, " + tsq + ") + " + boost + ")::float8"
		conds = append(conds, "(doc @@ "+tsq+" OR "+boost+" > 0)")
	}
	return `WITH m AS (SELECT *, ` + rank + ` AS rank FROM products WHERE ` + strings.Join(conds, " AND ") + `)`
}

func boostSQL(ids []string, args *sqlArgs) string {
	if len(ids) == 0 {
		return "0::float8"
	}
	return "(CASE WHEN category_id = ANY(" + args.add(ids) + ") THEN 1.0 ELSE 0.0 END)"
}

func orderBy(by search.Sort) string {
	switch by {
	case search.SortPriceAsc:
		return ` ORDER BY effective_price_minor ASC, catalog_item_id ASC`
	case search.SortPriceDesc:
		return ` ORDER BY effective_price_minor DESC, catalog_item_id ASC`
	case search.SortTitle:
		return ` ORDER BY title_sort COLLATE "C" ASC, catalog_item_id ASC`
	}
	return ` ORDER BY rank DESC, catalog_item_id ASC`
}

// keysetWhere keeps the rows strictly behind the cursor position.
func keysetWhere(req readmodel.SearchRequest, args *sqlArgs) string {
	c := req.Query.After
	if c == nil {
		return ""
	}
	id := args.add(c.ID)
	switch c.Sort {
	case search.SortPriceAsc:
		p := args.add(c.Price)
		return ` WHERE (effective_price_minor > ` + p + ` OR (effective_price_minor = ` + p + ` AND catalog_item_id > ` + id + `))`
	case search.SortPriceDesc:
		p := args.add(c.Price)
		return ` WHERE (effective_price_minor < ` + p + ` OR (effective_price_minor = ` + p + ` AND catalog_item_id > ` + id + `))`
	case search.SortTitle:
		t := args.add(c.Title)
		return ` WHERE (title_sort COLLATE "C" > ` + t + ` OR (title_sort COLLATE "C" = ` + t + ` AND catalog_item_id > ` + id + `))`
	}
	r := args.add(c.Rank)
	return ` WHERE (rank < ` + r + ` OR (rank = ` + r + ` AND catalog_item_id > ` + id + `))`
}
