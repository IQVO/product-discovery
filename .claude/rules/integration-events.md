---
paths:
  - "internal/adapters/**/kafka/**"
  - "apis/asyncapi*"
---

# Cross-service integration events (Kafka)

product-discovery PUBLISHES nothing (`publishes: []`): no outbox, no relay, no
`retailnetwork.product-discovery.events` topic. It CONSUMES eleven events from three
topics and writes only dead letters (`<topic>.dlq`). Full contract:
`docs/adr/0003-consumed-contracts-and-rebuild.md`,
`docs/adr/0004-cloudevents-envelope-and-type-catalogue.md` and `apis/asyncapi.yaml`
(a receive-only note of the fields this service reads).

## Events: CloudEvents 1.0 is MANDATORY

Every message consumed is a CloudEvents 1.0 event in structured content mode; read
`.claude/rules/fleet/cloudevents.md`. There is no flat envelope and no envelope toggle
(`TestNoEventEnvelopeToggleOrFlatEnvelope`).

- Decode only through `internal/adapters/kafka/cloudevents` (`Decode`); transport is
  `segmentio/kafka-go`.
- Consumers `Decode` (validates), dispatch on the FULL `type` string, ignore unknown
  types, dedupe on `id` through the inbox (`processed_events`, same transaction as the
  projection), dead-letter a fact that can never be folded (`shared.ErrInvalidFact`,
  classified in `internal/adapters/inbound/kafka/handlers.go`), and never crash or block
  the partition. A transient failure retries 5 times, then dead-letters.
- Per-key monotonic guards (listing `asOf`, content `revision`, price `version`, tree
  `version`) live in the domain fold rules, not in the adapter.
- Tests: `internal/adapters/inbound/kafka` (dispatch over the memory doubles, the run
  loop, dead letters), `cmd/api/wiring_integration_test.go` (testcontainers Kafka only).

### Consumed types

| `type` | topic | guard |
| --- | --- | --- |
| `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated` / `CatalogItemDiscontinued` | `retailnetwork.offer-availability.events` | idempotent |
| `com.retailnetwork.commerce.offer-availability.listing.ListingCreated` / `ListingSuppressed` / `ListingRestored` | `retailnetwork.offer-availability.events` | idempotent (ListingItemMap) |
| `com.retailnetwork.commerce.offer-availability.listing.ListingAvailabilityChanged` | `retailnetwork.offer-availability.events` | `asOf` |
| `com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished` / `ProductContentRetired` | `retailnetwork.product-content.events` | `revision` |
| `com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished` | `retailnetwork.product-content.events` | `version` |
| `com.retailnetwork.storefront.pricing.itemprice.ItemPriceChanged` / `ItemPriceWithdrawn` | `retailnetwork.pricing.events` | `version` |

## Consumer group ids

Three mandatory environment variables, read in `cmd/api/config.go`, never a literal
(`TestKafkaConsumerGroupNeverHardcodedInline`): `OFFER_AVAILABILITY_CONSUMER_GROUP`,
`PRODUCT_CONTENT_CONSUMER_GROUP`, `PRICING_CONSUMER_GROUP`. The consumers resume from
their committed offsets (they are not replay-on-start caches), so stable groups are
correct; the snapshot rebuild (`cmd/rebuild`) covers history beyond topic retention.
