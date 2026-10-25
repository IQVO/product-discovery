# 1. Product Discovery is a bounded context of the storefront domain

Status: Accepted (bootstrap). Design source: warehouse-docs `storefront/model/model.yaml`, context `product-discovery`.

## Context

The storefront is the e-commerce domain of the retail-network organisation: six bounded contexts, each its own
service and database. The read side of browsing and search: one ProductView per catalogue item (title, flags, content, category path, list and effective price, inStock, purchasable) and one CategoryView (tree plus purchasable counts), fed by events from offer-availability, product-content and pricing, searched with PostgreSQL full-text search and pg_trgm (D9), behind a port so a dedicated engine is a later adapter swap.

## Decision

- This repo owns the aggregates ; the invariants and error names are those of the model.
- Business decisions of the context:
- No aggregate in v1 (its ADR 0002 says so plainly): projections only.
- Search ranks title (A), category names (A), attributes (B), description (C) in a tsvector with a GIN index; pg_trgm similarity is the typo fallback; facets are category, price band and in-stock.
- Product-type queries also return category suggestions [6].
- Out-of-order safety comes from per-source monotonic guards (listing asOf, content revision, price version) on top of the CloudEvents-id inbox.
- Bootstrap is snapshot + catch-up: paged OHS reads of the network catalogue and the storefront's own pricing and content, then events; listing availability has no storefront OHS read, so the rebuild uses GET /listings as an in-org read on an ops-only path, never on a user request.
- Events use the storefront CloudEvents namespace `com.retailnetwork.storefront.product-discovery.<entity>.<Event>` on `retailnetwork.product-discovery.events`
  (analytics-only events on `.analytics`). Published: none (read side). Consumed: CatalogItemCreated, CatalogItemDiscontinued, CategoryTreePublished, ItemPriceChanged, ItemPriceWithdrawn, ListingAvailabilityChanged, ListingCreated, ListingRestored, ListingSuppressed, ProductContentPublished, ProductContentRetired. No `warehouse.*` topic is consumed or produced.
- The storefront calls only the network's Open Host Service routes (model inbound rows for `shopper / storefront`),
  never an in-org or restricted route; sibling data used in a decision is a local event-fed read model, except the
  calls the model lists under `sync_calls`.
- No authentication on REST, consistent with the fleet and the network. This context stores no PII and no card data.

## Consequences

Relationships and the full consumed-event list are in the model and in the AsyncAPI contract added with the first feature PR.
