# ADR 0003: Consumed contracts, per-key guards and the snapshot rebuild

## Status

Accepted (2026-10-25).

## Context

product-discovery is a Conformist of three producers and publishes nothing. Event shapes come from the
`publishes` rows of the producer contexts in the storefront model (pricing, product-content) and from the network
offer-availability contracts under warehouse-docs `network/`. `apis/asyncapi.yaml` is a receive-only note of the
fields this service reads; the producers' own files stay authoritative and unknown fields are ignored.

## Decision

### Consumed events (11) and their guards

| Topic | Type | Entity | Guard | Effect |
|---|---|---|---|---|
| `retailnetwork.offer-availability.events` | `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated` | catalogitem | stub or new | title, flags, ACTIVE |
| | `...catalogitem.CatalogItemDiscontinued` | catalogitem | idempotent | DISCONTINUED |
| | `...listing.ListingCreated` | listing | idempotent | listing row, ListingItemMap |
| | `...listing.ListingAvailabilityChanged` | listing | `asOf` strictly newer | quantity |
| | `...listing.ListingSuppressed` | listing | idempotent | listing SUPPRESSED (via ListingItemMap) |
| | `...listing.ListingRestored` | listing | idempotent | listing LIVE (via ListingItemMap) |
| `retailnetwork.product-content.events` | `com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished` | productcontent | `revision` strictly newer | content, bullets, media, category id |
| | `...productcontent.ProductContentRetired` | productcontent | `revision` not older | content RETIRED |
| | `...categorytree.CategoryTreePublished` | categorytree | `version` strictly newer | CategoryView replaced |
| `retailnetwork.pricing.events` | `com.retailnetwork.storefront.pricing.itemprice.ItemPriceChanged` | itemprice | `version` strictly newer | prices, currency |
| | `...itemprice.ItemPriceWithdrawn` | itemprice | `version` not older | price WITHDRAWN |

Dispatch is on the full type string. Each message runs in one transaction: insert the CloudEvents `id` into
`processed_events` (a duplicate is acknowledged), fold, recompute, save. A malformed fact
(`shared.ErrInvalidFact`) is dead-lettered; a stale one is acknowledged.

### Snapshot rebuild (S5, H7, H8)

Paged keyset reads, 500 per page, only from `cmd/rebuild`: offer-availability `GET /catalog` (OHS) and
`GET /listings` (ops-only in-org exception, `TestNoInOrgNetworkRoute` allowlists exactly this one); pricing
`GET /item-prices`; product-content `GET /product-contents` and `GET /categories`. `GetProductContent` is not
called. Rows are folded through the same rules as events, which seeds the guards (A6 in ADR 0002).

## Consequences

The overlap between snapshot and events is safe because every source carries a monotonic guard. The `/listings`
read never runs on a user request.
