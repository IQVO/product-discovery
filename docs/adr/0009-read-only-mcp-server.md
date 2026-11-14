# ADR 0009: a read-only MCP server

## Status

Accepted (2026-11-14). Extends [ADR 0007](./0007-a2a-inbound-adapter.md) (the A2A agent), which said this context
had no MCP surface to mirror; applies the fleet Agentic Standard v2 (`warehouse-docs`
`docs/architecture/agentic-standard.md`, AS-1, AS-3, AS-4 and AS-9, which names this context: the storefront contexts
that have an A2A agent but no MCP server gain one) and the fleet decision of one MCP server per bounded context.

## Context

Product Discovery is the read side of browsing and search ([ADR 0001](./0001-product-discovery-bounded-context.md)).
It had a REST API (`GET /products`, `GET /products/{catalogItemId}`, `GET /categories`) and an A2A agent
([ADR 0007](./0007-a2a-inbound-adapter.md)), but no MCP server, while the fleet's other contexts that serve agents
ship one so that a model host can call a context's reads as typed tools without a bespoke REST client.
[ADR 0005](./0005-fleet-observability-standard.md) said the observability standard covers `cmd/api` and `cmd/rebuild`
(and, from ADR 0007, `cmd/a2a`); the new binary follows the same standard. An MCP adapter is another driving adapter
over the read use cases that already back the REST GETs, not new domain.

## Decision

**A third driving adapter, a separate binary in the same image.** `internal/adapters/inbound/mcp` is built on the
official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`) and served over Streamable HTTP only (no stdio, no SSE)
by `cmd/mcp`, mounted at `/mcp` and `/`, with an open `GET /healthz`. The binary ships in the SAME image as `/app/mcp`
beside `/app/api`, `/app/a2a` and `/app/rebuild`. The Helm chart gains an `mcp:` component (Deployment, Service,
optional gateway HTTPRoute at `/api/mcp/product-discovery`), **default OFF** (`mcp.enabled: false`). `cmd/rebuild` is a
Job, not a long-running server, and gets no MCP.

**Two read tools, one per read the storefront needs; nothing else.** Each is a thin wrapper over a read use case that
already backs a REST GET and an A2A skill. The names are the A2A skill names and mirror the REST operation ids
(`searchProducts`, `getProduct`) so that the later Universal Commerce Protocol catalog binding (search and lookup)
maps onto them one to one. This ADR does NOT implement UCP: that is a later phase with its own decision.

| Tool | Input (lowerCamelCase, like REST) | Output | Authority | Backing use case | REST / A2A counterpart |
|---|---|---|---|---|---|
| `search_products` | `q?`, `categoryId?`, `inStock?`, `priceMin?`, `priceMax?`, `sort?`, `limit?` (1 to 500, default 100), `cursor?` | `items`, `facets`, `categorySuggestions?`, `nextCursor?` | L1 | `Queries.SearchProducts` | `GET /products`; A2A `search_products` |
| `get_product` | `catalogItemId` (required) | the product view | L1 | `Queries.GetProduct` | `GET /products/{catalogItemId}`; A2A `get_product` |

Paging is keyset-paged exactly like the REST route: the opaque `cursor` of the previous answer's `nextCursor`, repeated
with the same filters and sort; `nextCursor` is omitted on the last page. The input and output names are REST's
(`cursor`, `items`, `nextCursor`), not the retired `after` and `next`.

**Left out on purpose (not evidenced as needed, backed by a REST GET but not by a skill):** a `list_categories` tool
over `Queries.ListCategories` (`GET /categories`). The category tree already reaches an agent as the `categoryPath` of a
product, the `categorySuggestions` and the category facet of a search, as ADR 0007 decided for the A2A skills. Adding
it later is a reviewed act: the tool budget, the curated list and the golden file change together.

**READ-ONLY is structural.** The adapter holds a `Reads` interface of exactly the two read use cases and nothing that
can write; `cmd/mcp` wires only the read repositories (product, category, search): no unit of work, inbox, consumer,
outbox or Kafka client, and no sibling-context call. `TestToolSurface` pins the exact tool set and the absence of write
verbs in tool names; the write-seam sensor fails the build if the adapter references a write use case or a
transactional seam by name; `TestToolRegistryGolden` pins the whole advertised registry (names, descriptions,
annotations, input and output schemas, authority); the integration test proves the binary reads what the Postgres read
side holds. Optional inputs carry `,omitempty` so the SDK publishes them as optional
(`TestMCPOptionalToolInputsCarryOmitempty`).

**Authority (AS-1).** Every tool declares `Meta: authorityMeta(authorityL1)` under `com.warehouse/authority`
(returned verbatim by `tools/list`) and is annotated read-only, idempotent and closed-world: every tool is a pure read
of this context's own projections. There is no L2 (no tool recommends), no L3 or L4 (nothing writes) and no
`initiated_by` attribution (stamped only by write paths). The static sensor
`internal/architecture/mcp_authority_fitness_test.go` fails the build when a tool misses its declaration or a level
contradicts the read-only annotation.

**Complete surface (AS-3).** Exactly one resource and one prompt join the tools, both read-only and built on the same
read use cases:

- `warehouse://product-discovery/products/{catalogItemId}` (resource template, `application/json`): the product view
  of ONE catalogue item, the same `GetProduct` use case as the tool and the REST GET, a scoped read model and never a
  dump of the catalogue. A stub the item master has not announced is an error, like an unknown id.
- `find_products` (prompt): this context's operator and agent procedure. It takes one typed, required argument
  (`query`), names the tools and never calls them, and cannot write: how to search (filters, sort, paging with
  `nextCursor`), how to read the answer (prices are minor units of the stated currency, `purchasable` and `inStock` are
  facts of the projection, an item without a price is not yet priced), when to read one product, and what is out of
  reach (ordering, reserving stock, changing prices or content are not offered here).

**Stateless transport (AS-4).** The Streamable HTTP handler is built with `&mcp.StreamableHTTPOptions{Stateless: true}`:
no `Mcp-Session-Id` is issued, the server answers the 2026-07-28 revision's per-request handshake (`server/discover`)
and a legacy `initialize` still answers 200. Pinned by raw-POST tests.

**Errors.** A handler error becomes an `isError` tool result `"<slug>: <detail>"`, never a transport failure. The slugs
are the REST problem slugs for the errors these reads can raise: `not-found` (unknown id, a stub, an id over 128
characters or carrying a NUL), `validation-failed` (a bad search value), `invalid-limit` and `invalid-cursor` (REST's
400s), `malformed-request` (a NUL in a string argument, which PostgreSQL text cannot hold) and `internal-error`
(anything else; the cause is logged, never echoed).

**No sibling calls, no auth.** The server reads this context's own tables only, and has no auth layer (the fleet-wide
revert of 2026-09-11; `TestNoAuthMiddlewareReintroduced` keeps it out). Access control is the in-cluster ClusterIP
boundary.

**Composition root and observability.** `cmd/mcp` keeps what the two reads need from `cmd/api`, like `cmd/a2a`: the
embedded migrations (idempotent), the Postgres product and category repositories and the search port, the price-band
edges and the existing search counter. Environment: `DATABASE_URL` (required), `MCP_ADDR` (default `:8090`, the fleet's MCP port; 8080 is the api and 8093 the A2A agent),
`MIGRATIONS_DATABASE_URL`, `PRICE_BAND_EDGES` (must equal the api's so the facets agree), `LOG_LEVEL` and the OTel
variables; `OTEL_SERVICE_NAME` defaults to `product-discovery-mcp`. The router uses the fleet middleware order
(RequestID, otelchi, request-duration histogram, RequestLogger, Recoverer). No new metric is introduced.

## Consequences

- An agent host can search and read the storefront catalogue with typed tools, and gate on `com.warehouse/authority`:
  everything here is safe to call without approval.
- Three deployables read one database (api, a2a, mcp); each runs the same idempotent migrations, so any may boot first.
- Adding ANY write tool later is a new ADR (it would be L3, carry `initiated_by`, and amend this read-only charter) plus
  a golden and sensor-visible diff; this context has no write on its REST surface either.
- The Universal Commerce Protocol catalog binding stays a later phase; the tool names are chosen so the mapping is
  obvious.
- One more Deployment per release when enabled, one replica; `warehouse-infra` flips it on and the agent catalogue is
  regenerated in separate steps, after the image publishes `/app/mcp`.
