---
id: tools
title: MCP tools
sidebar_label: MCP tools
sidebar_position: 1
---

# MCP tools

`cmd/mcp` serves a **read-only** Model Context Protocol server over the same read use cases as REST
`GET /products` and `GET /products/{catalogItemId}` and the A2A skills
([ADR 0009](/docs/adr/0009-read-only-mcp-server)). Transport is Streamable HTTP, stateless, on `/mcp` and `/`
(`MCP_ADDR`, default `:8090`; through Kong `/api/mcp/product-discovery`, rewritten to `/mcp`). There is no
authentication: the surface is unauthenticated by fleet decision and access control is the in-cluster
ClusterIP boundary. The server is **off by default** in the chart (`mcp.enabled`).

Source: `internal/adapters/inbound/mcp/`, `cmd/mcp/`. The registry is pinned by a golden file
(`internal/adapters/inbound/mcp/testdata/tool_registry.golden.json`) and the surface by
`internal/architecture/mcp_*_test.go`.

## Authority

Every tool declares `_meta` `com.warehouse/authority` = `L1` (Agentic Standard v2: a pure read of this
context's own projections) and the annotations `readOnlyHint`, `idempotentHint` and `openWorldHint=false`.
No tool of a higher level exists, and no tool name carries a write verb.

## Tools

| Tool | Authority | Arguments | Answer |
| --- | --- | --- | --- |
| `search_products` | L1 | all optional, named as the `GET /products` parameters: `q`, `categoryId`, `inStock`, `priceMin`, `priceMax`, `sort`, `limit` (1 to 500, default 100), `cursor` | `{items, facets, categorySuggestions, nextCursor}` |
| `get_product` | L1 | `catalogItemId` (required) | the product view, as `GET /products/{catalogItemId}` |

Errors are tool errors (`isError`) that reuse the REST slugs: `validation-failed`, `invalid-limit`,
`invalid-cursor`, `not-found`. An infrastructure error is reported as a generic failure; its text (which
could carry a DSN) never reaches the model.

## Resource and prompt

| Kind | Name | Meaning |
| --- | --- | --- |
| Resource template | `warehouse://product-discovery/products/{catalogItemId}` (`application/json`) | the same view as `get_product` |
| Prompt | `find_products` (argument `query`, required) | the procedure for finding products: search, page with `nextCursor`, read the items that matter, interpret prices in minor units and `purchasable` |

## What is deliberately absent

- No write tool and no write seam in the adapter or in `cmd/mcp`.
- No `categories` tool: the category tree is a REST read (`GET /categories`) the shopper journey does not need
  through MCP; the facets and `categorySuggestions` of `search_products` cover narrowing.
- No Kafka, no consumer, no outbox: the server reads Postgres only.
