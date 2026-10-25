# ADR 0002: Projections, no aggregates, and assumptions

## Status

Accepted (2026-10-25).

## Context

The design source is warehouse-docs `storefront/model/model.yaml`, context `product-discovery`, with the FINAL
model-gap decisions of the storefront campaign (H1-H8) applied on top; they override the model where they differ.
This context has **no aggregate in v1**: it owns two projections and the search behaviour on top of them. This ADR
records the projections as built and every assumption (A1, A2, ...) taken where the model and the decisions are
silent. Each assumption is also reported to the orchestrator as a `model_gap`.

## Decision

### Projections (domain packages)

- **ProductView** (`internal/domain/productview`): one row per catalogue item, a plain value that facts are folded
  into. Item state (stub, ACTIVE, DISCONTINUED), title and flags; content state, revision, description, bullets,
  attributes, media and the primary `categoryId` (H3: the id only; the path is joined from CategoryView at query
  time); price state, version, currency, list and effective price; the listings of the item (state, quantity,
  `asOf`). `inStock` and `purchasable` are derived (PurchasableRule). The fold rules are the methods `Apply...`;
  each returns `Applied` or `Stale` and never rejects a well-formed fact.
- **CategoryView** (`internal/domain/categoryview`): one published tree version, replaced wholesale when the
  version is above the current one. Rolls purchasable counts up the tree; matches category names for product-type
  queries.
- **search** (`internal/domain/search`): the validated `Query`, the keyset `Cursor`, the fixed price `Bands` (H6:
  `<2500`, `2500-4999`, `5000-9999`, `10000-24999`, `25000+`, edges configurable), the `Facets` and the pg_trgm
  `Similarity`.
- **ListingItemMap** (H1) is a read-side lookup table `listing_items(listing_id, catalog_item_id)`, learned from
  ListingCreated (and from ListingAvailabilityChanged, which also carries the item) and used by ListingSuppressed
  and ListingRestored.
- **Stub rows** (H4, rule ErrNone): a listing, price or content fact for an item the item master has not announced
  upserts a stub row. A stub is invisible to every read; CatalogItemCreated completes it. A CatalogItemDiscontinued
  that arrives first is remembered, so the late create cannot revive the item.

### Assumptions

- **A1 Suppress/restore for an unknown listing.** ListingSuppressed/ListingRestored carry only `listingId`. When
  the ListingItemMap has no entry (the ListingCreated was lost or predates retention) the fact is acknowledged and
  logged as skipped, not retried or dead-lettered; the bootstrap rebuild repairs the listing. Both events share the
  listing key with ListingCreated, so on a healthy topic this cannot happen.
- **A2 Visibility.** `GET /products/{id}` returns every non-stub row, even a discontinued or unpurchasable one (the
  page renders "currently unavailable"). Search and browse list only rows with an ACTIVE item and PUBLISHED
  content; `purchasable` and `inStock` are returned and filterable but do not hide a row. A row without a price
  shows no price and is excluded by any price filter.
- **A3 Category filter and counts.** `categoryId` filters the category and all its descendants. A category's
  `purchasableCount` includes its descendants. A `categoryId` that is not in the tree matches nothing.
- **A4 Category names rank A without a tsvector copy.** H3 forbids denormalising category names into the product
  row, so the A-weight of category names is realised at query time: categories whose name covers every query word
  (plural-folded) boost the products in their subtree and make them match, on top of the `tsvector` (title A,
  attributes B, description C, GIN-indexed).
- **A5 Product-type query suggestions.** `categorySuggestions` are the categories whose name covers every query
  word, shallowest first, at most 5. The model does not specify a detection rule (H5 removes it).
- **A6 Snapshot guards.** The producer OHS reads carry no `updatedAt`; the per-source guard values they do carry
  seed the guards: listing `availability.asOf`, content `revision`, price `version` (and the tree `version`).
  Replayed events not newer than the snapshot are dropped by those guards. DRAFT contents are skipped.
- **A7 Facets.** Computed over the whole filtered set (not the page); empty buckets are omitted; the category
  facet value is the `categoryId`; price-band values are the band labels; in-stock values are `true`/`false`.
- **A8 Keyset pagination.** `after` is an opaque cursor (base64url JSON) holding the sort key of the last hit and
  `catalogItemId` as tiebreak, bound to the sort and to the matching mode. A cursor of another sort is a 422.
- **A9 Fallback trigger.** The pg_trgm fallback runs only on the first page of a text search whose full-text
  query matched zero rows (H6); the cursor then carries mode `trgm` so later pages stay in it.
- **A10 Content retire guard.** ProductContentRetired applies when its `revision` is not older than the folded
  one (a missing revision applies unconditionally); a later publish with a higher revision revives the content.
- **A11 Price withdraw guard.** ItemPriceWithdrawn applies when its `version` is not older than the folded one;
  ItemPriceChanged needs a strictly higher version, so a later price revives a withdrawn one. A withdrawn price
  keeps no amounts.
- **A12 Rebuild is an ops job.** `RebuildProjections` is the binary `cmd/rebuild` (a Kubernetes Job), not a REST
  route, so the ops-only `GET /listings` read can never run on a user request. It pages each source, seeds the
  projections through the same fold rules, and the Kafka consumers then resume from the committed offsets.
- **A13 No outbox.** The context publishes nothing, so it has no outbox table or relay; `/readyz` is a Postgres
  ping plus Kafka metadata with an empty non-nil topic list. The Kafka writer exists only for the dead-letter
  topic.

## Consequences

Tests state each assumption. Changing one is a new ADR.
