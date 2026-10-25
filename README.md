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
- Domain packages (`internal/domain`): `productview` (fold rules), `categoryview`, `search`, `shared`. Decisions: `docs/adr/`.

## Quality gate

`make check` (fast), `make check-all`, `make integration` (Docker), `make mutation` (every domain package), `make vuln`.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
