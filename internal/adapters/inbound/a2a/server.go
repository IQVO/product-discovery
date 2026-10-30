// Package a2a is the inbound Agent2Agent adapter: it exposes this bounded
// context to the fleet's agent ecosystem as a second driving adapter beside the
// REST one, over the SAME application-layer read use cases (usecases.Queries).
// It is built on the official A2A Go SDK (a2aproject/a2a-go/v2) and speaks
// JSON-RPC 2.0 over HTTP, with the Agent Card served at the well-known path.
//
// Per ADR 0007, A2A is the DELEGATION channel between bounded contexts: an
// agent client sends a goal-shaped skill request as a structured message part
// and this agent answers from its own read model through its own use cases. It
// is not a data-sync channel: facts between contexts still travel exclusively
// as CloudEvents on Kafka (ADR 0004).
//
// This agent is READ-ONLY. This context has no MCP adapter (ADR 0005 lists
// only cmd/api and cmd/rebuild), so there is no MCP charter to derive a write
// from; both skills are backed by the read use cases a REST GET already
// exposes (GET /products and GET /products/{catalogItemId}). This package
// never writes, never calls another bounded context and contains no outbound
// A2A client.
//
// Like the REST adapter, this package depends inward on the application layer
// (use cases) and the domain only, never on an outbound adapter. The
// composition root (cmd/a2a) wires the concrete Postgres read side into the
// use cases. Domain structs never leak across the message boundary.
package a2a

import (
	"context"
	"iter"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// Config carries the deployment-specific identity of this agent: the absolute
// URL clients use to reach its JSON-RPC endpoint (advertised on the Agent
// Card) and the card version. It is supplied by the composition root from
// A2A_AGENT_URL; the card MUST advertise a URL the fleet's agent clients can
// actually dial (in-cluster: http://<release>-a2a.<ns>.svc.cluster.local:8093/).
type Config struct {
	AgentURL string
	Version  string
}

// Deps is everything the A2A skills need, injected by the composition root.
// It carries the SAME read use cases the REST adapter uses; the adapter never
// constructs an outbound adapter itself, and never writes.
type Deps struct {
	// Queries are the existing read use cases (SearchProducts and GetProduct),
	// reused unchanged.
	Queries usecases.Queries
}

// Card builds this agent's Agent Card (ADR 0007). The surface is two curated,
// goal-shaped READ skills, each backed by a read use case a REST GET already
// exposes. The card declares no security schemes on purpose: this surface is
// unauthenticated by the same fleet-wide decision as REST (see the REST
// adapter's package comment); schemes are added here, not in a redesign, if
// that decision is ever reversed.
func Card(cfg Config) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "product-discovery",
		Description: "Product Discovery agent for the digital storefront: the read-only source of truth for what a shopper can find and see. It searches the product catalogue projection (title, category and attribute full-text with a typo fallback, category / price / in-stock filters, facets and category suggestions) and reads one product page view (content, category path, list and effective price, stock and purchasability). It never changes the catalogue, the prices or the offers, and it does not place orders or reserve stock.",
		Version:     cfg.Version,
		Provider: &a2a.AgentProvider{
			Org: "IQVO",
			URL: "https://github.com/IQVO/product-discovery",
		},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(cfg.AgentURL, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "text/plain"},
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
		Skills: []a2a.AgentSkill{
			{
				ID:          skillSearchProducts,
				Name:        "Search the product catalogue",
				Description: "Find purchasable products. Arguments (all optional): q (free text, at most 200 characters; a product-type query also returns categorySuggestions), categoryId (restrict to that category subtree), inStock (true / false), priceMin and priceMax (effective price in minor units, not negative), sort (relevance | price-asc | price-desc | title), limit (1 to 100, default 20) and after (the next cursor of a previous answer). Each hit carries catalogItemId, title, effectivePrice, primaryMedia, categoryPath, inStock and purchasable; the answer also carries the facets and a next cursor (null on the last page). A bad value is answered as a 422 validation-failed problem. Persists nothing.",
				Tags:        []string{"product", "search", "catalogue", "read"},
				InputModes:  []string{"application/json"},
				Examples: []string{
					`{"skill":"search_products","arguments":{"q":"kettle","inStock":true,"sort":"price-asc","limit":5}}`,
					`{"skill":"search_products","arguments":{"categoryId":"kettles","priceMax":10000}}`,
				},
			},
			{
				ID:          skillGetProduct,
				Name:        "Read one product page view",
				Description: "Read the full view of one catalogue item by catalogItemId: title, hazmat and fragile flags, description, bullets, attributes, media, categoryId and categoryPath (once content is published), list and effective price (once priced), inStock and purchasable. An item the item master has not announced yet is invisible: it answers a 404 problem, like an unknown id. Persists nothing.",
				Tags:        []string{"product", "catalogue", "read"},
				InputModes:  []string{"application/json"},
				Examples: []string{
					`{"skill":"get_product","arguments":{"catalogItemId":"I-1"}}`,
				},
			},
		},
	}
}

// NewExecutor returns the AgentExecutor that maps an incoming message to a
// skill and answers with a single agent Message (a message-only agent in A2A
// terms): both curated skills are transactional request/response reads, so
// there is no long-running Task lifecycle to track in this first A2A surface.
// The response carries the caller's contextId (and taskId when the client
// referenced one) so a delegating saga can group its interactions.
func NewExecutor(deps Deps) a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(answer(ctx, deps, ec), nil)
		}
	})
}

// Handler wires the executor into the transport-agnostic A2A request handler
// and wraps it in the JSON-RPC 2.0 HTTP handler, the only transport this
// agent ships (ADR 0007).
func Handler(executor a2asrv.AgentExecutor) http.Handler {
	return a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor))
}
