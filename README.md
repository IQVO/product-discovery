# product-discovery

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Product Discovery** (Supporting, storefront tier).

The read side of browsing and search: one ProductView per catalogue item (title, flags, content, category path, list and effective price, inStock, purchasable) and one CategoryView (tree plus purchasable counts), fed by events from offer-availability, product-content and pricing, searched with PostgreSQL full-text search and pg_trgm (D9), behind a port so a dedicated engine is a later adapter swap.

Status: built (read side, no aggregate). The design lives in [warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/) `storefront/model/model.yaml` (single source of truth).
This repository is part of the storefront domain: it consumes the retail network's Open Host Service routes and its
published `retailnetwork.*` events; it never calls an in-org or restricted network route.

Runtime: Go, hexagonal, real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` and the three `*_CONSUMER_GROUP` variables are
mandatory), CloudEvents 1.0 on every message. The service publishes nothing: it consumes eleven events from offer-availability,
product-content and pricing (inbox, per-key monotonic guards, dead letters) and serves search, browse and detail over REST.

## Surface

- `GET /products` (full-text search with pg_trgm typo fallback, filters, facets, keyset paging), `GET /products/{catalogItemId}`,
  `GET /categories`; `GET /healthz`, `GET /readyz`. Contract: `apis/openapi.yaml`. Consumed events: `apis/asyncapi.yaml`.
- `cmd/api` is the service; `cmd/rebuild` is the ops job that seeds the projections from the paged snapshot reads of the producers
  (`OFFER_AVAILABILITY_URL`, `PRICING_URL`, `PRODUCT_CONTENT_URL`). It never runs on a user request.
- `cmd/a2a` is the optional, READ-ONLY Agent2Agent agent ([ADR 0007](docs/adr/0007-a2a-inbound-adapter.md)): two skills,
  `search_products` and `get_product`, over the same read use cases as `GET /products` and `GET /products/{catalogItemId}`
  (adapter: `internal/adapters/inbound/a2a`). It needs only `DATABASE_URL` and `A2A_AGENT_URL`, has no Kafka and no write,
  and is unauthenticated like the REST surface. See [A2A agent](#a2a-agent).
- Domain packages (`internal/domain`): `productview` (fold rules), `categoryview`, `search`, `shared`. Decisions: `docs/adr/`.

## A2A agent

`cmd/a2a` serves an Agent2Agent agent (a2a-go v2, JSON-RPC over HTTP) that delegates two **read** goals to this context
(ADR 0007). A2A carries delegation, never data: facts between contexts still travel as CloudEvents on Kafka.

| | |
|---|---|
| Agent Card | `GET /.well-known/agent-card.json` |
| JSON-RPC | `POST /` and `POST /a2a` (method `SendMessage`, header `A2A-Version: 1.0`) |
| Liveness | `GET /healthz` |
| Listen address | `A2A_ADDR` (default `:8093`) |
| Advertised URL | `A2A_AGENT_URL` (required, absolute http(s) URL) |
| Skills | `search_products` (`q`, `categoryId`, `inStock`, `priceMin`, `priceMax`, `sort`, `limit`, `after`), `get_product` (`catalogItemId`) |

A request is a message part `{"skill":"get_product","arguments":{"catalogItemId":"I-1"}}`; the reply carries the envelope
`{skill, data | problem}` and a one-line text summary. Rejections come back in-band as problems with REST's statuses
(400 malformed, 404 not found, 422 validation failed, 500).

Kubernetes: `a2a.enabled=true` renders `<release>-a2a` (Deployment, Service, optional HTTPRoute `a2a.route.*` at
`/api/a2a/product-discovery`, rewritten to `/`). It is **off by default**.

## Observability

Logs, metrics and traces follow the fleet standard ([ADR 0005](docs/adr/0005-fleet-observability-standard.md)): JSON logs on stdout carry
`trace_id`/`span_id` and a `request_id`; `cmd/api`, `cmd/rebuild` and `cmd/a2a` export OTLP/gRPC to the Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`,
`OTEL_SERVICE_NAME`, `SERVICE_VERSION`, `ENVIRONMENT`; export never blocks, so no Collector costs telemetry, nothing else) with HTTP request
duration, Go runtime metrics and the business counter `discovery.searches.served` (`outcome`, `reason`); the consumers continue the trace
carried in the `traceparent` header of each consumed event.

## Quality gate

`make check` (fast), `make check-all`, `make integration` (Docker), `make mutation` (every domain package), `make vuln`.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
