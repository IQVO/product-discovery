// Package mcp is the inbound Model Context Protocol adapter: it exposes this
// bounded context to the AI ecosystem as a third driving adapter (beside REST
// and A2A) over the same application-layer read use cases. It is built on the
// official MCP Go SDK and served over Streamable HTTP only.
//
// Per docs/adr/0009-read-only-mcp-server.md the surface is READ-ONLY: every
// tool reads the storefront catalogue projection (search_products,
// get_product); folding the upstream feeds, the rebuild and every other write
// stay on the Kafka consumers and the ops job, and governance_test.go fails the
// build on a write-verb tool name and on a write seam referenced from this
// package. This package depends inward on the application layer and the domain
// only, never on an outbound adapter or the REST adapter; nothing else imports
// it but cmd/mcp, the composition root that wires the Postgres read side into
// the use cases. There is no auth of any kind (fleet-wide revert 2026-09-11):
// access control is the in-cluster ClusterIP boundary.
package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName and serverVersion identify this server in the MCP initialize
// handshake.
const (
	serverName    = "product-discovery-mcp"
	serverVersion = "1.0.0"
)

// instructions is what an MCP client is told about this server at initialize.
const instructions = "Product Discovery of the retail network (read-only): the storefront's read model of what a shopper can find and see. " +
	"Search the catalogue with search_products (optional free text with a typo fallback, categoryId, inStock, priceMin and priceMax in minor units, " +
	"sort, limit 1 to 500 and cursor: keyset-paged, pass nextCursor back as cursor with the same filters) and read one product page view with " +
	"get_product (content, category path, list and effective price, stock and purchasability). The product resource " +
	"(warehouse://product-discovery/products/{catalogItemId}) loads the same view without a tool call, and the find_products prompt hands you " +
	"this context's procedure. Nothing here changes data: the catalogue, prices and offers belong to other contexts, and ordering or reserving " +
	"stock are not offered."

// NewServer builds the MCP server for this bounded context with every tool,
// resource and workflow prompt registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions},
	)
	deps.registerTools(server)
	deps.registerResources(server)
	deps.registerPrompts(server)
	return server
}

// Handler returns the Streamable HTTP handler for the MCP server.
func Handler(server *mcp.Server) http.Handler {
	// Stateless (Agentic Standard v2, AS-4): no Mcp-Session-Id is issued, so
	// the server also answers the 2026-07-28 revision's per-request
	// handshake (server/discover) while legacy initialize clients keep
	// working. Nothing here relies on cross-call session state.
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}
