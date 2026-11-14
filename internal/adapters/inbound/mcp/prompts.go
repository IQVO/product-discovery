package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// findProductsSOP is the operator and agent procedure the find_products prompt
// hands to the model (Agentic Standard v2, AS-3). Per the charter, prompts
// encode how to interpret results, when to hand over and what "done" means;
// this one names tools but NEVER calls them, and it cannot write: every tool it
// names is read-only (ADR 0009) and nothing in this context is orderable
// through MCP.
const findProductsSOP = `You are helping a shopper find products in the storefront catalogue of the retail network. Use only the MCP tools of product-discovery; never assume the catalogue — read it.

Procedure:
1. Call search_products(q = "QUERY"). Add a filter only when the shopper stated it: categoryId (that category and its subtree), inStock (true or false), priceMin and priceMax (effective price in MINOR units of the currency: 2500 is 25.00), sort (relevance, price-asc, price-desc or title; relevance when omitted) and limit (1 to 500; 100 when omitted). A free-text query that names a product type also returns categorySuggestions: offer them to narrow the search. A misspelling is tolerated: when a first page finds nothing the search retries by similarity.
2. Read the page. Every item carries catalogItemId, title, effectivePrice (omitted until the item is priced), categoryPath, inStock and purchasable; facets show how the whole result splits by category, price band and stock. When nextCursor is present there is more: call search_products again with the SAME filters and sort and cursor = nextCursor, and stop when nextCursor is absent. Never invent or edit a cursor.
3. For the one or two items that matter, call get_product(catalogItemId) for the description, bullets, attributes, media, listPrice against effectivePrice and the hazmat and fragile flags. The resource warehouse://product-discovery/products/{catalogItemId} loads the same view without a tool call.

Interpretation:
- Prices are minor units with their currency code; effectivePrice is the current price of this projection and listPrice the reference price it is compared with. An item without a price is not priced yet: say so, do not guess one.
- purchasable is a fact this context derives from the item's published content, price and availability: never call an item buyable when it is false. inStock false means no available quantity is known to this projection.
- The projection follows other contexts by events, so it can lag them by seconds; state what you read, not what you expect.
- not-found is an unknown id, or an item the item master has not announced yet: it is no evidence that the product never existed. An empty page is a valid answer: widen the filters (drop inStock, then the price bounds) before concluding nothing matches.
- validation-failed, invalid-limit and invalid-cursor are fixed by correcting the argument named in the error, not by retrying unchanged.

Out of reach: this server cannot order, reserve stock, change a price or edit content, and it holds no delivery dates or per-node availability. Do not promise them; they belong to other contexts.

Done means: you have answered with the matching products (title, price with its currency, purchasable and inStock) from the pages you actually read, said whether more pages exist, and named anything you could not check.`

// registerPrompts adds the workflow prompt (the procedure of this context).
// Read-only by construction: a prompt returns text.
func (d Deps) registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "find_products",
		Description: "Procedure for finding products for a shopper: search with the stated filters, page with nextCursor, read the items that matter, interpret prices in minor units and purchasability, and know what this read-only context cannot do.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "query",
			Description: "what the shopper is looking for, e.g. steel kettle",
			Required:    true,
		}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		query := strings.TrimSpace(req.Params.Arguments["query"])
		if query == "" {
			query = "<query>"
		}
		return &mcp.GetPromptResult{
			Description: "How to find products: search with the stated filters, page with nextCursor, read the items that matter, report prices in minor units and purchasability as facts.",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: strings.ReplaceAll(findProductsSOP, "QUERY", query)},
			}},
		}, nil
	})
}
