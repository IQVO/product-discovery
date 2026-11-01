---
id: domain-events
title: Domain events
sidebar_label: Domain events
sidebar_position: 10
---

# Domain events

Every event this context consumes, from `apis/asyncapi.yaml` and the consumers in
`internal/adapters/inbound/kafka`. The context **publishes nothing**: no `retailnetwork.product-discovery.events`
topic is ever written and no analytics topic exists. The summary table with guards is on the
[event catalogue](/docs/api-reference/events); this page adds the envelope, the ordering rules and the
failure semantics.

## Envelope

Every consumed Kafka message is a **CloudEvents 1.0 event in structured JSON mode**, minted by its producer.
`internal/adapters/kafka/cloudevents.Decode` accepts nothing else (the retired flat envelope is refused and the
message is skipped).

| Attribute | Value (per producer) |
| --- | --- |
| `specversion` | `1.0` |
| `id` | a UUID minted once by the producer; a redelivery carries the same id, the inbox key |
| `source` | `/retailnetwork/offer-availability`, `/retailnetwork/product-content` or `/retailnetwork/pricing` |
| `type` | `com.retailnetwork.<tier>.<producer>.<entity>.<EventName>`: tier `commerce` for `offer-availability`, `storefront` for the two siblings |
| `subject` | the producer's root id; the Kafka key is the item id (item, content, price) or the listing id (listing) |
| `time` | the instant the fact happened |
| `datacontenttype` | `application/json` |
| `dataschema` | `urn:retailnetwork:<producer>:events:<EventName>:v1` |

Source: `internal/adapters/kafka/cloudevents/cloudevents.go`, `apis/asyncapi.yaml`.

## Consumption rules

- **Idempotent on the id:** the id is inserted into `processed_events` in the same transaction as the fold; a
  repeat is acknowledged without effect.
- **Dispatch on the full `type` string**, never on a suffix; unknown types are ignored.
- **Ordering:** within one producer, events with the same key arrive in order. Across keys and producers
  nothing is promised, so the per-source guards sit on top of the inbox: listing `asOf`, content `revision`,
  price `version`, tree `version`. A fact for an item whose `CatalogItemCreated` has not landed creates an
  invisible stub that the create completes.
- **Tolerant reader:** only the fields below are read; others are ignored.
- **Failure:** an undecodable payload or an unusable value is dead-lettered at once to `<topic>.dlq`; a
  transient failure is retried up to 5 times, then dead-lettered.

## Published

None. A read side with no events out keeps every write path free of a dependency on this context. The cost is
that search analytics (query terms, zero-result and fallback rates) have no event to ride on; the
`discovery.searches.served` counter is the only signal.

## Consumed from offer-availability

Topic `retailnetwork.offer-availability.events`, group `OFFER_AVAILABILITY_CONSUMER_GROUP`.

| Event | `data` fields read | What the context does |
| --- | --- | --- |
| `CatalogItemCreated` | `catalogItemId`, `title`, `hazmat`, `fragile` | Creates or completes the row: title and flags, item `ACTIVE`. Dimensions, weight and GTIN are not read |
| `CatalogItemDiscontinued` | `catalogItemId` | Item `DISCONTINUED`: `purchasable` goes false. The row stays, rendered as currently unavailable |
| `ListingCreated` | `listingId`, `catalogItemId`, `nodeId` | Learns the listing to item mapping; adds a `LIVE` listing with no quantity |
| `ListingAvailabilityChanged` | `listingId`, `catalogItemId`, `nodeId`, `availableQty`, `asOf` | Sets the quantity under the `asOf` guard; recomputes `inStock` and `purchasable`. `basis`, `feedId` and `ownerRef` are not read |
| `ListingSuppressed` | `listingId` | Listing `SUPPRESSED`; never counts towards `inStock`. The item comes from the stored mapping. The network's reason is not read |
| `ListingRestored` | `listingId` | Listing `LIVE` again |

## Consumed from product-content

Topic `retailnetwork.product-content.events`, group `PRODUCT_CONTENT_CONSUMER_GROUP`.

| Event | `data` fields read | What the context does |
| --- | --- | --- |
| `ProductContentPublished` | `catalogItemId`, `revision`, `description`, `bullets`, `attributes`, `media`, `primaryCategoryId` | Replaces the content and the category id under the `revision` guard; re-indexes the search document; content `PUBLISHED` |
| `ProductContentRetired` | `catalogItemId`, `revision` (optional) | Content `RETIRED`: the row stops being searchable and purchasable |
| `CategoryTreePublished` | `version`, `categories[{categoryId, parentId, name, path}]` | Replaces the whole tree under the `version` guard; purchasable counts are re-derived. No product row is re-indexed |

## Consumed from pricing

Topic `retailnetwork.pricing.events`, group `PRICING_CONSUMER_GROUP`.

| Event | `data` fields read | What the context does |
| --- | --- | --- |
| `ItemPriceChanged` | `catalogItemId`, `currency`, `listPriceMinor`, `effectivePriceMinor`, `version` | Sets both prices under the `version` guard; price `ACTIVE` |
| `ItemPriceWithdrawn` | `catalogItemId`, `version` | Price `WITHDRAWN`, amounts cleared; a withdrawal at a newer version is recorded even when already withdrawn |

## Example envelope

The event the purchasable verdict turns on, `ListingAvailabilityChanged`, one listing dropping to zero (ids
synthetic, values illustrative):

```json
{
  "specversion": "1.0",
  "id": "9a1c7b52-3d84-4f0e-8b7a-2c5d9e0f1a3b",
  "source": "/retailnetwork/offer-availability",
  "type": "com.retailnetwork.commerce.offer-availability.listing.ListingAvailabilityChanged",
  "subject": "LST-40987",
  "time": "2026-10-25T09:41:07Z",
  "datacontenttype": "application/json",
  "dataschema": "urn:retailnetwork:offer-availability:events:ListingAvailabilityChanged:v1",
  "data": {
    "listingId": "LST-40987",
    "catalogItemId": "CI-100234",
    "nodeId": "NODE-MSP-14",
    "availableQty": 0,
    "asOf": "2026-10-25T09:41:05Z"
  }
}
```

The context reads five fields of `data`, inserts the `id` into `processed_events`, applies the `asOf` guard
and, if the item's last live listing just went to zero, flips `inStock` and `purchasable` to false in the same
transaction. No personal data travels in any of these events.

Source: `internal/adapters/inbound/kafka/offer_availability.go`, `internal/domain/productview/fold_listing.go`.
