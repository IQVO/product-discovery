---
id: use-cases
title: Use cases
sidebar_label: Use cases
sidebar_position: 1
---

# Use cases

The context has **no aggregate and no command use case**. Its application services live in
`internal/application/usecases`: three **queries** (called over REST and over A2A), one **fold** per consumed
event type (the projector, called by the Kafka consumers), and the ops-run **rebuild**. Nothing is ever
published and no route is a write, so no `Idempotency-Key` exists. Diagrams for each flow are on
[Sequence diagrams](/docs/ddd/sequence-diagrams); payloads are on [Domain events](/docs/ddd/domain-events) and
[Events](/docs/api-reference/events).

Source: `internal/application/usecases/{queries,consume,consume_offer,consume_catalogue,rebuild}.go`,
`internal/domain/{productview,categoryview,search}`.

## At a glance

| Use case | Trigger | Inbound adapter | Events raised |
| --- | --- | --- | --- |
| `SearchProducts` | shopper via `storefront-bff`, or an agent | REST `GET /products`; A2A skill `search_products` | none |
| `GetProduct` | shopper, agent | REST `GET /products/{catalogItemId}`; A2A skill `get_product` | none |
| `ListCategories` | shopper | REST `GET /categories` | none |
| Fold: `ApplyCatalogItemCreated`, `ApplyCatalogItemDiscontinued`, `ApplyListingCreated`, `ApplyListingAvailabilityChanged`, `ApplyListingStateChanged` (suppressed or restored) | a CloudEvent on `retailnetwork.offer-availability.events` | Kafka consumer `offer-availability` | none (a projection write) |
| Fold: `ApplyProductContentPublished`, `ApplyProductContentRetired`, `ApplyCategoryTreePublished` | a CloudEvent on `retailnetwork.product-content.events` | Kafka consumer `product-content` | none |
| Fold: `ApplyItemPriceChanged`, `ApplyItemPriceWithdrawn` | a CloudEvent on `retailnetwork.pricing.events` | Kafka consumer `pricing` | none |
| `Rebuilder.Run` (RebuildProjections) | an operator, never a user request | `cmd/rebuild` (Helm Job or one-off pod) | none |

## How reads and folds commit

- **Reads** run outside any transaction on one query against the projections; they write nothing, so they are
  retryable and safe to load-balance. Errors are problems: `not-found` (404) and `validation-failed` (422);
  anything else is `internal-error` (500). The A2A skills reuse the same `Queries` and the same classification.
- **Folds** run in one unit of work (`Consumer.handle`): claim the CloudEvents `id` in `processed_events`
  (a repeat is acknowledged without effect), load the row `FOR UPDATE` (a fresh stub when none), apply one
  pure fold rule, and save the row when the rule returned `Applied`. A `Stale` outcome (the per-source guard
  or an already-known state) saves nothing but the inbox claim. A serialization failure
  (`ports.ErrConcurrentUpdate`) re-runs the whole unit up to 5 times.
- **The rebuild** seeds through the same fold rules, one row per unit of work, without the inbox.

## SearchProducts

- **Inputs:** `q` (free text, at most 200 characters after whitespace is collapsed), `categoryId`, `inStock`,
  `priceMin`, `priceMax` (minor units, not negative), `sort` (`relevance` default, `price-asc`,
  `price-desc`, `title`), `after` (opaque cursor), `limit` (1 to 100, default 20; 0 is refused).
- **Invariants:**
  - `priceMin` must not exceed `priceMax`; a cursor must belong to the same sort and the same kind of query
    (text or browse), otherwise `validation-failed` with field `after`.
  - A query with no text is a browse; with text it is full-text (`tsvector`: title A, attributes B,
    description and bullets C) plus a query-time boost for categories whose name covers every query word.
  - Only rows with an `ACTIVE` item and `PUBLISHED` content are listed; unpurchasable rows are listed and
    flagged. Price filters and price sorts list only rows with an `ACTIVE` price (ADR 0002 A14).
  - `categoryId` filters that category and every descendant; an id outside the tree matches nothing.
  - On the **first page** of a text search whose full-text query matched nothing, the search is retried once
    in trigram mode (similarity 0.3 or more); the cursor then carries mode `trgm` and later pages stay in it.
  - Facets (`category`, `price-band`, `in-stock`) are tallied over the whole filtered set, not the page; empty
    buckets are dropped. Band edges come from `PRICE_BAND_EDGES` (default `2500,5000,10000,25000`).
  - A product-type query also returns up to 5 `categorySuggestions`, shallowest first.
- **Output:** `hits`, `facets`, `categorySuggestions`, `next` (null on the last page).
- **Metric:** exactly one increment of `discovery.searches.served`: `failed` (with `reason`), `empty` (a first
  page without hits) or `served`.
- **Events raised:** none.

## GetProduct

- **Inputs:** `catalogItemId` (path; longer than 128 characters is a 404).
- **Invariants:** a **stub** row (the item master has not announced the item) is invisible: `not-found`.
  Every other row is returned, including a discontinued or unpurchasable one. Content fields and the category
  path appear only while the content is `PUBLISHED`; prices only while the price is `ACTIVE`. `inStock` and
  `purchasable` are computed from the row.
- **Output:** the ProductView with `categoryPath` joined from CategoryView at query time (so a category rename
  shows at once).
- **Events raised:** none.

## ListCategories

- **Inputs:** none.
- **Invariants:** the tree is one published version; before the first publication the answer is `version: 0`
  and an empty list. `purchasableCount` is the number of purchasable products in the category and all its
  descendants.
- **Events raised:** none.

## Folds: offer-availability

| Fold | Input | Rule and invariants | Outcome |
| --- | --- | --- | --- |
| `ApplyCatalogItemCreated` | `catalogItemId`, `title`, `hazmat`, `fragile` | Title trimmed, 1 to 500 characters (else invalid fact). Completes a stub: title and flags set, item state `ACTIVE` unless a discontinue already arrived. A row that already has a title is left alone (the title is immutable) | `Applied` or `Stale` |
| `ApplyCatalogItemDiscontinued` | `catalogItemId` | Item state `DISCONTINUED`, never purchasable again. On a stub it is remembered so a late create cannot revive the item | `Applied`, or `Stale` when already discontinued |
| `ApplyListingCreated` | `listingId`, `catalogItemId`, `nodeId` | Learns `listingId` to `catalogItemId`; adds a `LIVE` listing with no quantity, or marks an existing one's state as known | `Applied` or `Stale` |
| `ApplyListingAvailabilityChanged` | `listingId`, `catalogItemId`, `nodeId`, `availableQty`, `asOf` | Quantity not negative, `asOf` not zero. **Guard:** applied only when `asOf` is strictly newer than the listing's last `asOf`. An unknown listing is added `LIVE`. Learns the listing to item mapping | `Applied` or `Stale` |
| `ApplyListingStateChanged` | `listingId`, suppressed or restored | Looks the item up in the listing map; an unknown listing is logged (WARN) and skipped, not retried (ADR 0002 A1). A suppressed listing never counts towards `inStock` | `Applied` or `Stale` |

## Folds: product-content

| Fold | Input | Rule and invariants | Outcome |
| --- | --- | --- | --- |
| `ApplyProductContentPublished` | `catalogItemId`, `revision`, `description`, `bullets`, `attributes`, `media`, `primaryCategoryId` | `revision` at least 1; category id valid when present. **Guard:** applied only when `revision` is strictly higher than the one folded; it also revives content that a retire ended | `Applied` or `Stale` |
| `ApplyProductContentRetired` | `catalogItemId`, `revision` (0 when absent) | Content becomes `RETIRED`. Revision 0 always applies; a retire of an older revision than the one folded is stale | `Applied` or `Stale` |
| `ApplyCategoryTreePublished` | `version`, `categories` (`categoryId`, `parentId`, `name`, `path`) | Valid unique ids, non-empty names, every parent present, no cycles (else invalid fact). **Guard:** the tree is replaced only when `version` is strictly higher than the current one; the tree is stored under a row lock | applied or ignored |

## Folds: pricing

| Fold | Input | Rule and invariants | Outcome |
| --- | --- | --- | --- |
| `ApplyItemPriceChanged` | `catalogItemId`, `currency` (three capital letters), `listPriceMinor`, `effectivePriceMinor` (not negative), `version` | **Guard:** `version` at least 1 and strictly higher than the one folded; it revives a withdrawn price | `Applied` or `Stale` |
| `ApplyItemPriceWithdrawn` | `catalogItemId`, `version` | Price becomes `WITHDRAWN` with amounts cleared, unless a newer version was folded; a withdrawal at a newer version is recorded even when already withdrawn, so a replayed older change cannot revive it | `Applied` or `Stale` |

Every fold recomputes `inStock` and `purchasable` from the row, and the search document when an indexed
column changed. Nothing is rejected for being out of order: the worst case is a skipped stale fact.
Unusable facts (an id outside the allowed alphabet or over 64 characters, a negative quantity, a zero
`asOf`, a bad price or revision, an invalid tree) are dead-lettered by the consumer.

## RebuildProjections

- **Trigger:** an operator ([Runbook](/docs/operations/runbook#running-the-rebuild)); never a user request.
- **Inputs:** the four base URLs and `DATABASE_URL` (all required).
- **Flow:** `catalog` (offer-availability `GET /catalog`), `listings` (the ops-only `GET /listings`), `item
  prices` (pricing `GET /item-prices`), `product contents` (product-content `GET /product-contents`),
  `categories` (product-content `GET /categories`), in that order, 500 rows per page. A catalogue row also
  marks a discontinued item; a content row in state `DRAFT` is skipped; a withdrawn price row is folded as a
  withdrawal; an unpublished tree (version below 1) is skipped.
- **Invariants:** a snapshot never rewinds a listing state a state fact already set (a known listing only
  moves by the `asOf` guard, ADR 0002 A16); the per-source guards drop every replayed event that is not newer,
  so the consumers can resume from their committed offsets afterwards. A row that cannot be folded is skipped
  and counted; a failing page aborts the run; re-running is safe.
- **Events raised:** none.
