package usecases

import (
	"context"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// MaxSuggestions bounds the category suggestions of a product-type query.
const MaxSuggestions = 5

// Queries are the three read use cases of apis/openapi.yaml.
type Queries struct{ Deps }

// Product is the answer of GetProduct: the row plus the category path joined
// from CategoryView.
type Product struct {
	View         productview.View
	CategoryPath []string
}

// HitView is one search hit with its category path.
type HitView struct {
	readmodel.Hit
	CategoryPath []string
}

// SearchPage is the answer of SearchProducts.
type SearchPage struct {
	Hits        []HitView
	Facets      []search.Facet
	Suggestions []categoryview.Category
	Next        string
}

// CategoryList is the answer of ListCategories.
type CategoryList struct {
	Version    int64
	Categories []categoryview.Counted
}

// GetProduct returns the row of a catalogued item. A stub (an item the item
// master has not announced) is invisible: ports.ErrNotFound.
func (q Queries) GetProduct(ctx context.Context, catalogItemID string) (Product, error) {
	v, err := q.Products.Get(ctx, catalogItemID)
	if err != nil {
		return Product{}, err
	}
	if v.IsStub() {
		return Product{}, ports.ErrNotFound
	}
	cats, err := q.Categories.Current(ctx)
	if err != nil {
		return Product{}, err
	}
	return Product{View: v, CategoryPath: cats.Names(v.CategoryID)}, nil
}

// ListCategories returns the current tree with purchasable counts; before the
// first publication it is empty.
func (q Queries) ListCategories(ctx context.Context) (CategoryList, error) {
	cats, err := q.Categories.Current(ctx)
	if err != nil {
		return CategoryList{}, err
	}
	direct, err := q.Products.PurchasableByCategory(ctx)
	if err != nil {
		return CategoryList{}, err
	}
	return CategoryList{Version: cats.Version(), Categories: cats.Counted(direct)}, nil
}

// SearchProducts validates the input, resolves the category tree into the
// request, runs the search, retries in trigram mode when a first full-text
// page finds nothing (H6), and joins the category paths.
func (q Queries) SearchProducts(ctx context.Context, in search.Input) (SearchPage, error) {
	page, firstPage, err := q.searchProducts(ctx, in)
	q.recordSearch(ctx, page, firstPage, err)
	return page, err
}

// searchProducts is the search proper; firstPage reports that the request
// carried no cursor, so an empty answer means "nothing matches", not "past the
// last page".
func (q Queries) searchProducts(ctx context.Context, in search.Input) (_ SearchPage, firstPage bool, _ error) {
	query, err := search.NewQuery(in)
	if err != nil {
		return SearchPage{}, false, err
	}
	firstPage = query.After == nil
	out, err := q.runSearch(ctx, query)
	return out, firstPage, err
}

func (q Queries) runSearch(ctx context.Context, query search.Query) (SearchPage, error) {
	cats, err := q.Categories.Current(ctx)
	if err != nil {
		return SearchPage{}, err
	}
	req := q.request(query, cats)
	res, err := q.Search.Search(ctx, req)
	if err != nil {
		return SearchPage{}, err
	}
	if req.Mode == search.ModeFTS && query.After == nil && len(res.Hits) == 0 {
		req.Mode = search.ModeTrgm
		if res, err = q.Search.Search(ctx, req); err != nil {
			return SearchPage{}, err
		}
	}
	out := SearchPage{Facets: res.Facets}
	for _, h := range res.Hits {
		out.Hits = append(out.Hits, HitView{Hit: h, CategoryPath: cats.Names(h.CategoryID)})
	}
	if res.Next != nil {
		out.Next = res.Next.Encode()
	}
	if !query.Browse() {
		out.Suggestions = cats.Suggestions(query.Text, MaxSuggestions)
	}
	return out, nil
}

func (q Queries) request(query search.Query, cats categoryview.View) readmodel.SearchRequest {
	req := readmodel.SearchRequest{Query: query, Mode: search.ModeBrowse, Bands: q.bands()}
	if !query.Browse() {
		req.Mode = search.ModeFTS
		req.BoostCategoryIDs = cats.Match(query.Text)
	}
	if query.After != nil {
		req.Mode = query.After.Mode
	}
	if query.CategoryID != "" {
		req.CategoryIDs = cats.Subtree(query.CategoryID)
		if req.CategoryIDs == nil {
			req.CategoryIDs = []string{}
		}
	}
	return req
}
