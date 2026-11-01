---
id: events
title: Event catalogue
sidebar_label: Event catalogue
sidebar_position: 2
---

# Event catalogue

`product-discovery` **consumes eleven event types from three topics and publishes none**: there is no outbox,
no relay, no `retailnetwork.product-discovery.events` topic and no analytics topic. Its only Kafka producer is
the dead-letter writer. Every message is a CloudEvents 1.0 event in **structured mode**: the Kafka value is
the whole JSON envelope. Envelopes are decoded only in `internal/adapters/kafka/cloudevents`
([ADR 0004](/docs/adr/0004-cloudevents-envelope-and-type-catalogue)); a message that is not a valid
CloudEvents 1.0 event is logged at WARN and skipped. The contract is `apis/asyncapi.yaml` (it records only the
fields this service reads; each producer's own AsyncAPI is authoritative and unknown fields are ignored).
`TestEventCatalogueMatchesContract` keeps the code's type list and the spec in step. Field-level notes are on
[Domain events](/docs/ddd/domain-events).

Source: `internal/adapters/inbound/kafka/{offer_availability,product_content,pricing,handlers}.go`.

## Published

None. The dead-letter record is the only thing the service writes to Kafka: it copies the failed message to
`<topic>.dlq` with the original key, value and headers plus `x-dlq-source-topic`, `x-dlq-source-partition`,
`x-dlq-source-offset`, `x-dlq-error` and `x-dlq-failed-at` ([Runbook](/docs/operations/runbook#kafka-topics-consumer-groups-and-dead-letters)).

## Consumed

Dispatch is on the **full** `type` string; any other type on these topics is ignored. The CloudEvents `id` is
the idempotency key (`processed_events`). The Kafka key is the item id for item, content and price events and the listing id for listing events (`apis/asyncapi.yaml`); the examples carry the same value as the CloudEvents `subject`.

### offer-availability: `retailnetwork.offer-availability.events` (group `OFFER_AVAILABILITY_CONSUMER_GROUP`)

Type prefix `com.retailnetwork.commerce.offer-availability.`

| Type suffix | `data` fields read | Guard | Effect |
| --- | --- | --- | --- |
| `catalogitem.CatalogItemCreated` | `catalogItemId`, `title`, `hazmat`, `fragile` | the title of a known row is immutable | creates or completes the row; item `ACTIVE` |
| `catalogitem.CatalogItemDiscontinued` | `catalogItemId` | none | item `DISCONTINUED`; never purchasable again |
| `listing.ListingCreated` | `listingId`, `catalogItemId`, `nodeId` | none | learns the listing; adds a `LIVE` listing |
| `listing.ListingAvailabilityChanged` | `listingId`, `catalogItemId`, `nodeId`, `availableQty`, `asOf` | `asOf` strictly newer than the listing's | sets the quantity; recomputes `inStock` |
| `listing.ListingSuppressed` | `listingId` | none | listing `SUPPRESSED`; never counts towards `inStock` |
| `listing.ListingRestored` | `listingId` | none | listing `LIVE` again |

### product-content: `retailnetwork.product-content.events` (group `PRODUCT_CONTENT_CONSUMER_GROUP`)

Type prefix `com.retailnetwork.storefront.product-content.`

| Type suffix | `data` fields read | Guard | Effect |
| --- | --- | --- | --- |
| `productcontent.ProductContentPublished` | `catalogItemId`, `revision`, `description`, `bullets[]`, `attributes[{name, value, unit}]`, `media[{mediaId, url, altText, role}]`, `primaryCategoryId` | `revision` strictly higher than the folded one | replaces the content and the category id; content `PUBLISHED`; re-indexes the search document |
| `productcontent.ProductContentRetired` | `catalogItemId`, `revision` (optional) | an older revision than the folded one is stale; absent means 0 and always applies | content `RETIRED`; `purchasable` and `searchable` become false |
| `categorytree.CategoryTreePublished` | `version`, `categories[{categoryId, parentId, name, path}]` | `version` strictly higher than the current tree | replaces the whole tree; counts are re-derived |

### pricing: `retailnetwork.pricing.events` (group `PRICING_CONSUMER_GROUP`)

Type prefix `com.retailnetwork.storefront.pricing.itemprice.`

| Type suffix | `data` fields read | Guard | Effect |
| --- | --- | --- | --- |
| `ItemPriceChanged` | `catalogItemId`, `currency`, `listPriceMinor`, `effectivePriceMinor`, `version` | `version` strictly higher | sets both prices; price `ACTIVE` (revives a withdrawn price) |
| `ItemPriceWithdrawn` | `catalogItemId`, `version` | not older than the folded version | price `WITHDRAWN`, amounts cleared |

### What makes a fact unusable

A fact that can never be folded is dead-lettered at once (no retry): `data` that does not decode; an id that
is not 1 to 64 characters of `A-Z a-z 0-9 . _ : -` starting with a letter or digit; a title that is empty or
over 500 characters; a negative `availableQty`, `listPriceMinor` or `effectivePriceMinor`; a zero `asOf`; a
`currency` that is not three capital letters; a `version` or `revision` below 1 (a retire may carry 0); an
invalid category tree (duplicate id, empty name, unknown parent, a parent cycle). Everything else the guards
decide is silent: a stale fact is acknowledged without effect.

## Example envelope

```json
{
  "specversion": "1.0",
  "id": "5b0f8a52-3c1e-4d7a-9f4e-2a6d1c9e7b31",
  "source": "/retailnetwork/offer-availability",
  "type": "com.retailnetwork.commerce.offer-availability.listing.ListingAvailabilityChanged",
  "subject": "LST-000184",
  "time": "2026-10-22T14:05:12Z",
  "datacontenttype": "application/json",
  "dataschema": "urn:retailnetwork:offer-availability:events:ListingAvailabilityChanged:v1",
  "data": {
    "listingId": "LST-000184",
    "catalogItemId": "CI-004512",
    "nodeId": "NODE-VWS-FC01",
    "availableQty": 120,
    "asOf": "2026-10-22T14:00:00Z"
  }
}
```

Source: `apis/asyncapi.yaml` (the example of the message), the same envelope the [Quickstart](/docs/overview/quickstart)
publishes.

## Consumer groups and dead-letter topics

| Topic | Consumer group variable | Dead-letter topic |
| --- | --- | --- |
| `retailnetwork.offer-availability.events` | `OFFER_AVAILABILITY_CONSUMER_GROUP` | `retailnetwork.offer-availability.events.dlq` |
| `retailnetwork.product-content.events` | `PRODUCT_CONTENT_CONSUMER_GROUP` | `retailnetwork.product-content.events.dlq` |
| `retailnetwork.pricing.events` | `PRICING_CONSUMER_GROUP` | `retailnetwork.pricing.events.dlq` |

The group ids are mandatory, stable and have no default; the chart and `warehouse-infra` choose
`product-discovery-offer-availability`, `product-discovery-product-content` and `product-discovery-pricing`.
