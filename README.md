# product-discovery

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Product Discovery** (Supporting, storefront tier).

The read side of browsing and search: one ProductView per catalogue item (title, flags, content, category path, list and effective price, inStock, purchasable) and one CategoryView (tree plus purchasable counts), fed by events from offer-availability, product-content and pricing, searched with PostgreSQL full-text search and pg_trgm (D9), behind a port so a dedicated engine is a later adapter swap.

Status: under construction. The design lives in [warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/) `storefront/model/model.yaml` (single source of truth).
This repository is part of the storefront domain: it consumes the retail network's Open Host Service routes and its
published `retailnetwork.*` events; it never calls an in-org or restricted network route.

Runtime: Go, hexagonal, real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` are mandatory), CloudEvents 1.0 on every message
(`com.retailnetwork.storefront.product-discovery.<entity>.<Event>`), transactional outbox.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
