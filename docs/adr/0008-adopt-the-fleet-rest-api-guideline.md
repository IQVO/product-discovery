# ADR 0008: Adopt the fleet REST API guideline

## Status

Accepted (2026-11-01).

## Context

The fleet REST API guideline (R1-R8: noun routes, idempotent `POST`/`PATCH`, `409 idempotency-key-reused`,
`Idempotency-Replayed`, keyset paging with `{items, nextCursor}`) is mandatory for every `apis/openapi.yaml`
and is enforced by a Spectral rule in the `api-lint` job. This context is read-only
([ADR 0001](./0001-product-discovery-bounded-context.md)): it has no `POST` and no `PATCH`, and its two list
`GET`s are the only operations the guideline touches.

## Decision

- **Ruleset.** `.spectral/functions/restGuideline.js` and the `rest-guideline` rule in `.spectral.yaml`;
  `spectral lint apis/openapi.yaml --ruleset .spectral.yaml --fail-severity=warn` exits 0.
- **Routes.** No route is renamed: `/products`, `/products/{catalogItemId}` and `/categories` are already
  plural nouns. No `Idempotency-Key` exists because no route writes; this is stated rather than assumed
  (nothing to wrap, no key store, no `Idempotency-Replayed`).
- **`GET /products` is paged** with `limit` (1-500, default 100, was 1-100 default 20) and `cursor`, and answers
  `{items, facets, categorySuggestions, nextCursor}` (was `{hits, facets, categorySuggestions, next}`).
  `nextCursor` is absent on the last page. `facets` and `categorySuggestions` are the search-style siblings the
  guideline allows (STANDARD 3.3).
- **Cursor format (assumption).** The cursor stays the domain's own keyset `search.Cursor` (sort, matching mode,
  rank/price/title, `catalogItemId` tiebreak; [ADR 0002](./0002-product-discovery-assumptions.md)), still opaque
  base64url JSON. The fleet `page` package is copied under `internal/adapters/inbound/http/page` and supplies
  `Limit`, `Unknown`, the envelope errors and the problem mapping; its `Position` encoding is not used because
  relevance ranking needs the rank and the mode in the cursor. The query orders by (sort key, `catalogItemId`).
- **Problems.** `400 invalid-limit` (empty, repeated, not an integer, outside 1-500), `400 invalid-cursor`
  (repeated, undecodable, or a cursor of another sort or matching mode; was `422`), `400
  unknown-query-parameter` (any undeclared parameter, so the retired `after`, `offset` and `nextToken` fail
  loudly). Filter errors (`sort`, prices, `inStock`, `q`) stay `422 validation-failed`.
- **Exemption.** `GET /categories` carries `x-pagination-exempt: view`: it is one versioned CategoryView tree
  with purchasable counts rolled up, a single composite resource that is meaningless in slices.
- **A2A.** The `search_products` skill keeps its own tool schema (`after`, `hits`, `next`, in-band `422`); only
  its `limit` bound follows the domain (1-500). `TestParity_TheAgentExposesExactlyWhatRESTExposes` maps the
  envelope names and pins the one deliberate difference (paging problems).
- **Consumers in this repo.** The rebuild source clients (`internal/adapters/outbound/sources`) send `cursor`
  (was `after`) and read `nextCursor` (was `next` for `GET /item-prices` and `GET /product-contents`) until it
  is absent. `GET /categories` of product-content is read as a single tree, unchanged.

## Consequences

- Breaking for every reader of `GET /products` (storefront-bff): `cursor` instead of `after`, `items` instead of
  `hits`, `nextCursor` instead of `next`. The REST cutover set deploys together.
- No Schemathesis job exists in this repo, so no `schemathesis.toml` is needed.
