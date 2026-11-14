package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// Reads is the read-only slice of the application layer: EXACTLY the two read
// use cases the REST adapter exposes for the storefront (GET /products and
// GET /products/{catalogItemId}) and the A2A agent exposes as skills;
// usecases.Queries implements it. The adapter holds this and nothing that can
// write (docs/adr/0009): the composition root (cmd/mcp) wires no write seam at
// all.
type Reads interface {
	GetProduct(ctx context.Context, catalogItemID string) (usecases.Product, error)
	SearchProducts(ctx context.Context, in search.Input) (usecases.SearchPage, error)
}

// Deps is everything the MCP tools need, injected by the composition root.
type Deps struct {
	Reads Reads
}

const (
	// maxLimit is the largest page the search accepts (REST: limit 1 to 500).
	maxLimit = 500
	// maxIDLength bounds a catalogue item identifier; a longer one cannot name
	// an item (the REST adapter's maxIDLength).
	maxIDLength = 128
)

// --- inputs -------------------------------------------------------------------

// searchInput mirrors GET /products: the same query parameters under the same
// names. Every field is optional, so each carries omitempty (the SDK publishes
// a field without it as required).
type searchInput struct {
	Q          string `json:"q,omitempty" jsonschema:"optional free-text query, at most 200 characters; a product-type query also returns categorySuggestions"`
	CategoryID string `json:"categoryId,omitempty" jsonschema:"optional category id: restrict to that category and its subtree"`
	InStock    *bool  `json:"inStock,omitempty" jsonschema:"optional filter: true for in-stock products only, false for products that are not in stock"`
	PriceMin   *int64 `json:"priceMin,omitempty" jsonschema:"optional lower bound of the effective price in minor units of the currency, not negative"`
	PriceMax   *int64 `json:"priceMax,omitempty" jsonschema:"optional upper bound of the effective price in minor units of the currency, not negative"`
	Sort       string `json:"sort,omitempty" jsonschema:"optional ordering: relevance, price-asc, price-desc or title; omitted means relevance"`
	Limit      *int   `json:"limit,omitempty" jsonschema:"optional page size, 1 to 500; omitted means 100"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"optional opaque cursor: the nextCursor of the previous page, passed back unchanged with the same filters and sort"`
}

// productInput mirrors GET /products/{catalogItemId}: the path parameter.
type productInput struct {
	CatalogItemID string `json:"catalogItemId" jsonschema:"the catalogue item id of the product, e.g. I-1"`
}

// --- registry -----------------------------------------------------------------

// registerTools adds the two read-only tools, one per read GET the storefront
// needs. Every tool is annotated read-only, idempotent and closed-world (the
// annotation literal is inlined per tool so the fleet's static authority
// sensor can prove ReadOnlyHint) and declares its authority level L1 under
// com.warehouse/authority (Agentic Standard v2, ADR 0009): each is a pure read
// of this context's own projections. TestToolSurface pins the set and fails
// the build on any write-verb name.
func (d Deps) registerTools(server *mcp.Server) {
	closedWorld := false

	addTool(server, &mcp.Tool{
		Name: "search_products",
		Description: "Search the storefront catalogue for purchasable products: optional free text (full-text over title, category and attributes " +
			"with a typo fallback), categoryId, inStock, priceMin and priceMax (effective price in minor units), sort (relevance, price-asc, " +
			"price-desc, title), limit (1 to 500, default 100) and cursor. Returns items (catalogItemId, title, effectivePrice, primaryMedia, " +
			"categoryPath, inStock, purchasable), facets (category, price band, in-stock), categorySuggestions for a product-type query and " +
			"nextCursor (omitted on the last page): pass it back as cursor with the same filters and sort for the next page. Read-only; a bad " +
			"value fails with validation-failed, a bad limit with invalid-limit and a foreign cursor with invalid-cursor.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.searchProducts)

	addTool(server, &mcp.Tool{
		Name: "get_product",
		Description: "Read the full view of one catalogue item by catalogItemId: title, hazmat and fragile flags, description, bullets, attributes, " +
			"media, categoryId and categoryPath (once content is published), list and effective price (once priced), inStock and purchasable. " +
			"Read-only; an unknown id, an item the item master has not announced yet or an id that cannot name an item fails with not-found.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.getProduct)
}

// addTool registers one tool. A handler error is returned to the SDK as the
// handler's error, which the SDK turns into an isError tool result (never a
// transport/protocol failure), carrying the error text.
func addTool[In, Out any](
	server *mcp.Server,
	tool *mcp.Tool,
	handle func(context.Context, In) (Out, error),
) {
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := handle(ctx, in)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		return nil, out, nil
	})
}

// --- handlers -----------------------------------------------------------------

// searchProducts is the discovery tool, through the same SearchProducts use
// case GET /products uses.
func (d Deps) searchProducts(ctx context.Context, in searchInput) (searchView, error) {
	if containsNUL(in.Q, in.CategoryID, in.Sort, in.Cursor) {
		// The REST adapter's 400 for a NUL in the query string: PostgreSQL text
		// cannot hold U+0000 and a statement carrying one aborts the transaction.
		return searchView{}, toolError("malformed-request", "a NUL character (U+0000) is not allowed in the arguments")
	}
	req := search.Input{
		Text: in.Q, CategoryID: in.CategoryID, InStock: in.InStock,
		PriceMin: in.PriceMin, PriceMax: in.PriceMax, Sort: in.Sort, After: in.Cursor,
	}
	if in.Limit != nil {
		if *in.Limit < 1 || *in.Limit > maxLimit {
			// REST: a limit outside 1..500 (0 included) is refused, not defaulted.
			return searchView{}, toolError("invalid-limit", "limit must be an integer between 1 and 500")
		}
		req.Limit = *in.Limit
	}
	page, err := d.Reads.SearchProducts(ctx, req)
	if err != nil {
		return searchView{}, mapError(ctx, err)
	}
	return toSearchView(page), nil
}

// getProduct is the single-product read tool, through the same GetProduct use
// case GET /products/{catalogItemId} uses.
func (d Deps) getProduct(ctx context.Context, in productInput) (productView, error) {
	product, err := d.lookupProduct(ctx, in.CatalogItemID)
	switch {
	case isNotFound(err):
		return productView{}, notFound(in.CatalogItemID)
	case err != nil:
		return productView{}, mapError(ctx, err)
	}
	return toProductView(product), nil
}

// lookupProduct reads one product for the tool and the resource alike. An
// identifier that cannot name an item (empty, over-long or carrying a NUL) is
// ports.ErrNotFound without a repository call, like the REST adapter's 404.
func (d Deps) lookupProduct(ctx context.Context, id string) (usecases.Product, error) {
	if id == "" || len(id) > maxIDLength || containsNUL(id) {
		return usecases.Product{}, ports.ErrNotFound
	}
	return d.Reads.GetProduct(ctx, id)
}

func containsNUL(values ...string) bool {
	for _, v := range values {
		if strings.ContainsRune(v, 0) {
			return true
		}
	}
	return false
}
