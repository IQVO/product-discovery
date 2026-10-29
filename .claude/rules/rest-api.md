---
paths:
  - "internal/adapters/inbound/http/**"
  - "apis/openapi*.yaml"
  - "apis/openapi/**"
---

# REST API (inbound adapter)

Every route -> use case mapping, kept in sync with `apis/openapi.yaml` (routes live
in `internal/adapters/inbound/http/server.go`). This file is documentation for an
agent, not generated: keep it manually accurate. The surface is read-only.

- `GET /products?q=&categoryId=&inStock=&priceMin=&priceMax=&sort=&after=&limit=` -> `Queries.SearchProducts`
- `GET /products/{catalogItemId}` -> `Queries.GetProduct` (a stub row is a 404)
- `GET /categories` -> `Queries.ListCategories`
- `GET /healthz` (liveness) and `GET /readyz` (drain gate + Postgres ping + Kafka metadata)

## Conventions

- Error shape: RFC 9457 `application/problem+json`, `type` = `/problems/<slug>`
  (catalogue in `errors.go`): `not-found` (404), `validation-failed` (422, a bad `sort`,
  `limit`, bound, boolean or cursor), `malformed-request` (400, a NUL in the query),
  `resource-not-found` (404, a NUL in the path). There is no write route, so no
  `Idempotency-Key`.
- Auth: none. Fleet-wide auth was deliberately reverted 2026-09-11; do not add
  bearer/JWT/API-key middleware (`TestNoAuthMiddlewareReintroduced`).
