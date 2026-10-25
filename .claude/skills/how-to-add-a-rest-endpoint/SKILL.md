---
name: how-to-add-a-rest-endpoint
description: Add or change a REST endpoint in this service in the hexagonal order (domain rule, use case, port, handler, apis/openapi.yaml, godog scenario). Use when touching internal/adapters/inbound/http, apis/openapi.yaml, or exposing a use case over HTTP.
---

# How to add a REST endpoint

Follow this order: domain first, adapter last. A handler written before the
rule it relies on validates nothing and gets bypassed. The surface of this service
is READ-ONLY (three queries); a write route would be a new architectural decision
(ADR) because the context is a projection.

The worked example is `GET /products/{catalogItemId}`:
`internal/application/usecases/queries.go` `GetProduct` ->
`internal/adapters/inbound/http/handlers.go` `handleGetProduct`, rendered by
`dto.go` `productDTO`. The most involved one is `GET /products` (`SearchProducts`,
`handleSearch`, `searchInput`).

## 1. Domain first

Does the rule already exist in `internal/domain/<productview|categoryview|search|shared>/`?
A handler holds no business logic. A new filter or sort belongs to `search.Query`
(`internal/domain/search/query.go`, validated in `NewQuery`, `ErrValidation` becomes the 422
`validation-failed`), with a table test in the same package. Pure Go: no I/O, no clock.
Then re-run the mutation gate for that package (`gremlins unleash ./internal/domain/<pkg>`).

## 2. Application: the use case and the port

Add it to `internal/application/usecases/queries.go`; a query holds only its read ports
(`Deps`). If the search needs a new column or sort, extend `readmodel.SearchRequest`, implement
it in `internal/adapters/outbound/postgres/search.go` AND `internal/adapters/outbound/memory/search.go`,
and add the case to `internal/adapters/outbound/repocontract/searching_cases.go` so both
run it. Unit-test the use case in `internal/application/usecases/queries_test.go`.

## 3. Adapter: the handler

In `internal/adapters/inbound/http/`: add the route to `NewRouter` in `server.go`, write the
handler in `handlers.go` (parse, call the use case, encode through a DTO in `dto.go`,
`writeError` for every failure), map a new domain `Err` in `errors.go` `problemCatalogue`
(kebab-case slug, the status the OpenAPI documents) and assert status and slug in
`products_test.go` or `search_test.go`.

## 4. Contract: apis/openapi.yaml

Describe the operation and every problem it can return, then lint:
`spectral lint apis/openapi.yaml --ruleset .spectral.yaml` (the `api-lint` CI job runs the
same). Keep `.claude/rules/rest-api.md` and the README in step.

## 5. Behaviour

Add the happy path and each refusal to `features/*.feature` (`search.feature`,
`categories.feature`, `folding.feature`; the steps are in `internal/bdd/steps_*_test.go`).
The real middleware, Postgres and Kafka are proven by `cmd/api/wiring_integration_test.go`.

## 6. Verify before opening the PR

`make check`, `make coverage`, `make bdd`,
`go test -tags=integration ./cmd/... -race -count=1 -timeout 900s`, and the spectral lint above.
