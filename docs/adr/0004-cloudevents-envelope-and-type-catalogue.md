# ADR 0004: CloudEvents envelope and type catalogue

## Status

Accepted (2026-10-25).

## Context

Every Kafka message in the retail network is a CloudEvents 1.0 event in structured mode. This ADR is the catalogue
of the types product-discovery **publishes**: none (`publishes: []`). The types it consumes are listed in ADR 0003
and in `apis/asyncapi.yaml`.

## Decision

- Nothing is published on `retailnetwork.product-discovery.events` or `.analytics`; there is no outbox.
- The only message this service writes to Kafka is a dead letter: the original CloudEvents bytes on
  `retailnetwork.product-discovery.dlq`, with the failure reason in Kafka headers.
- Consumed envelopes are decoded only through `internal/adapters/kafka/cloudevents`; the dispatch key is the full
  `type`, the idempotency key the `id`.
- Source namespace of this service (should it ever publish): `com.retailnetwork.storefront.product-discovery.<entity>.<Event>`,
  source `/retailnetwork/product-discovery`, dataschema `urn:retailnetwork:product-discovery:events:<Event>:v1`.

## Consequences

`TestEventCatalogueMatchesContract` finds no type declared for this service and passes by construction.
