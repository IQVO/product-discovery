---
id: overview
title: API overview
sidebar_label: API overview
slug: /api-reference
sidebar_position: 1
---

# API overview

`cmd/api` serves the REST routes below on `HTTP_ADDR` (default `:8080`); `cmd/a2a` serves the A2A agent on
`A2A_ADDR` (default `:8093`). The contracts are `apis/openapi.yaml` and `apis/asyncapi.yaml`, pinned by
[ADR 0001](/docs/adr/0001-product-discovery-bounded-context) and
[ADR 0002](/docs/adr/0002-product-discovery-assumptions). The service is a read side: **it has no write route**
(hence no `Idempotency-Key`) and **publishes no event**. Everything is unauthenticated, by fleet decision
(`TestNoAuthMiddlewareReintroduced`).

Source: `internal/adapters/inbound/http/server.go`, `internal/adapters/inbound/http/handlers.go`,
`cmd/a2a/router.go`, `internal/adapters/inbound/a2a/`.

## Interfaces

| Interface | Source of truth | Notes |
| --- | --- | --- |
| REST | `apis/openapi.yaml` (OpenAPI 3.0.3, version 1.0.0) | The pages under *REST* in the sidebar are generated from the spec with `npm run gen-api-docs`; do not edit them by hand. The spec names one server, `http://localhost:8080`. Through Kong in the kind cluster the routes are under `/api/product-discovery` (prefix stripped) |
| Events | `apis/asyncapi.yaml` (AsyncAPI 3.0.0, version 1.0.0) | The service **consumes** eleven event types and publishes none; see the [event catalogue](/docs/api-reference/events) and [Domain events](/docs/ddd/domain-events) |
| MCP | none | There is no `cmd/mcp` and no MCP adapter |
| A2A | [ADR 0007](/docs/adr/0007-a2a-inbound-adapter), `internal/adapters/inbound/a2a`, `cmd/a2a` | JSON-RPC 2.0 on `/` and `/a2a`, Agent Card at `/.well-known/agent-card.json`, port 8093 (through Kong `/api/a2a/product-discovery`, rewritten to `/`). Two read-only skills, no write skill: `search_products` and `get_product` |

## Endpoints

| Tag | Endpoint | Operation id | Success | Errors |
| --- | --- | --- | --- | --- |
| Products | `GET /products` | `searchProducts` | `200` `{hits, facets, categorySuggestions, next}` | `422 validation-failed`; `400 malformed-request` (NUL in the query) |
| Products | `GET /products/{catalogItemId}` | `getProduct` | `200` the product view | `404 not-found` (unknown, stub or over-long id) |
| Categories | `GET /categories` | `listCategories` | `200` `{version, categories[]}`, empty at version 0 | none |
| Health | `GET /healthz` | not in the spec | `200` `{"status":"ok"}` always while the process runs | none |
| Health | `GET /readyz` | not in the spec | `200` `{"status":"ready"}` | `503 {"status":"not_ready"}` while draining; `503 dependency-unavailable` (`detail` `not ready: postgres` or `kafka`) |
| A2A | `POST /` and `POST /a2a` on `cmd/a2a` | not in the spec | the skill answer in a reply message | problems travel in the reply envelope |
| A2A | `GET /.well-known/agent-card.json`, `GET /healthz` on `cmd/a2a` | not in the spec | the Agent Card / `{"status":"ok"}` | none |

### `GET /products` parameters

| Parameter | Type | Rule |
| --- | --- | --- |
| `q` | string | Free text; at most 200 characters after whitespace is collapsed. Empty means browse |
| `categoryId` | string | The category and all its descendants; an id outside the tree matches nothing |
| `inStock` | boolean | Exactly `true` or `false` |
| `priceMin`, `priceMax` | integer | Effective price in minor units, not negative, `priceMin` at most `priceMax`. Only priced rows match |
| `sort` | string | `relevance` (default), `price-asc`, `price-desc`, `title` |
| `after` | string | The opaque `next` of the previous page; bound to the same `sort` and the same kind of query |
| `limit` | integer | 1 to 100, default 20; `0` is refused |

Response shape: each hit has `catalogItemId`, `title`, `effectivePrice` (`amountMinor`, `currency`),
`primaryMedia`, `categoryId`, `categoryPath`, `inStock`, `purchasable`. `facets` always holds three entries in
the order `category`, `price-band` and `in-stock`; empty buckets are dropped; price-band values are labels such
as `<2500`, `2500-4999`, `25000+` (edges from `PRICE_BAND_EDGES`). `categorySuggestions` (up to 5) appears for
text queries. `next` is `null` on the last page.

## A2A agent

`cmd/a2a` is a **message-only, read-only** agent built on the official `a2a-go` SDK
([ADR 0007](/docs/adr/0007-a2a-inbound-adapter)). Streaming is not advertised and the Agent Card declares no
security scheme (the surface is unauthenticated). The same read use cases back REST and A2A, so answers match;
`cmd/a2a`'s parity test compares both surfaces.

| Skill | Arguments | Backed by | Answer |
| --- | --- | --- | --- |
| `search_products` | the query parameters of `GET /products` under the same names: `q`, `categoryId`, `inStock`, `priceMin`, `priceMax`, `sort`, `after`, `limit`, all optional | `Queries.SearchProducts` | `data.search` with `hits`, `facets`, `categorySuggestions`, `next` |
| `get_product` | `catalogItemId` (required) | `Queries.GetProduct` | `data.product`, the product view |

A request is a message whose first part is a Data part (or a Text part holding the same JSON):

```json
{"skill": "search_products", "arguments": {"q": "kettle", "inStock": true, "sort": "price-asc", "limit": 5}}
```

The reply is an agent message with a Data part holding the envelope `{skill, data}` or `{skill, problem}` and a
Text part with a one-line summary. Decoding is strict: an unknown field is refused, not ignored. A problem is
a business answer, never a JSON-RPC error, and uses the REST slugs: `malformed-request` (400: no parts, no
`skill`, unknown skill or field, NUL character, trailing data), `not-found` (404), `validation-failed` (422)
and `internal-error` (500). The Agent Card carries the version (`SERVICE_VERSION`) and advertises
`A2A_AGENT_URL`, so that URL must be dialable by clients.

## Conventions in the contract

- **Errors** are RFC 9457 `application/problem+json` with `type` `/problems/<slug>`, `title`, `status`,
  `detail` and `instance`. This surface uses `malformed-request`, `not-found`, `validation-failed`,
  `internal-error` and (readiness only) `dependency-unavailable`; a NUL in the URL path answers
  `resource-not-found` (404). The slugs and their causes are on
  [Troubleshooting](/docs/operations/troubleshooting#rest-problem-types).
- **Money** is `{amountMinor, currency}`: minor units of an ISO-style three-letter currency.
- **Paging** is keyset-based: `limit` and the opaque `after`/`next` cursor. A cursor is bound to a sort and to
  a matching mode (browse, full-text or trigram) and is refused with `422` otherwise.
- **Visibility.** A stub row (the item master has not announced the item) is invisible. `GET /products/{id}`
  returns every other row, even a discontinued or unpurchasable one; search lists only active items with
  published content, flagged with `purchasable` and `inStock`.
- **Auth:** none.
- **Idempotency:** nothing to retry unsafely; every route is a `GET`.

## Spec versus code

- `apis/openapi.yaml` lists the `400 malformed-request` and the NUL-path `404 resource-not-found` answers on
  no operation; the router returns them (`internal/adapters/inbound/http/nul.go`).
- `/healthz`, `/readyz` and the A2A endpoints are not in the OpenAPI document.
- The OpenAPI `Problem.type` description says "slug, for example validation-failed"; the service emits the
  path form `/problems/validation-failed`.
