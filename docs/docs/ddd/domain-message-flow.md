---
id: domain-message-flow
title: Domain message flow
sidebar_label: Domain message flow
sidebar_position: 5
---

# Domain message flow

Following [Domain Message Flow Modelling](https://github.com/ddd-crew/domain-message-flow-modelling): the key
scenarios as numbered flows of queries (`qry`) and events (`evt`) across contexts. There is **no command** in
this context and no event leaves it. Detailed step-by-step interactions are on
[Sequence diagrams](/docs/ddd/sequence-diagrams).

## 1. Shopper search

```mermaid
flowchart LR
  SH(["Shopper"])
  BFF["storefront-bff"]
  PD["product-discovery"]
  DB[("PostgreSQL<br/>products, category_view")]
  SH -->|"1 type a query"| BFF
  BFF -->|"2 qry GET /products?q=..."| PD
  PD -->|"3 read tree, run full-text search,<br/>retry with pg_trgm if empty"| DB
  DB -->|"4 rows, facet tallies"| PD
  PD -->|"5 200 hits, facets, suggestions, next"| BFF
  BFF -->|"6 render results"| SH
```

Source: `internal/application/usecases/queries.go`, `internal/adapters/inbound/http/handlers.go`.

## 2. The availability feed

```mermaid
flowchart LR
  OA["offer-availability"]
  K[("Kafka<br/>offer-availability.events")]
  C["consumer<br/>offer-availability"]
  PD["usecases.Consumer"]
  DB[("PostgreSQL")]
  OA -->|"1 evt ListingAvailabilityChanged"| K
  K -->|"2 fetch one message"| C
  C -->|"3 decode CloudEvent, dispatch on type"| PD
  PD -->|"4 claim id, lock row, fold under asOf guard,<br/>recompute inStock and purchasable, save"| DB
  C -->|"5 commit offset after the transaction"| K
```

Source: `internal/adapters/inbound/kafka/offer_availability.go`, `internal/application/usecases/consume_offer.go`.

## 3. The content, tree and price feeds

```mermaid
flowchart LR
  PC["product-content"]
  PR["pricing"]
  K1[("product-content.events")]
  K2[("pricing.events")]
  C1["consumer product-content"]
  C2["consumer pricing"]
  PD["usecases.Consumer"]
  DB[("PostgreSQL")]
  PC -->|"1 evt ProductContentPublished,<br/>ProductContentRetired, CategoryTreePublished"| K1
  PR -->|"2 evt ItemPriceChanged, ItemPriceWithdrawn"| K2
  K1 --> C1
  K2 --> C2
  C1 -->|"3 revision / version guard"| PD
  C2 -->|"4 version guard"| PD
  PD -->|"5 save row and search document,<br/>or replace the tree"| DB
```

Source: `internal/adapters/inbound/kafka/{product_content,pricing}.go`, `internal/application/usecases/consume_catalogue.go`.

## 4. A message that cannot be applied

```mermaid
flowchart LR
  K[("source topic")]
  C["consumer"]
  DLQ[("topic.dlq")]
  K -->|"1 fetch"| C
  C -->|"2 undecodable data or unusable fact:<br/>dead-letter at once"| DLQ
  C -->|"3 database error: retry up to 5 attempts<br/>with 200 ms to 5 s backoff, then dead-letter"| DLQ
  C -->|"4 commit past the message<br/>after the DLQ publish succeeded"| K
```

Source: `internal/adapters/inbound/kafka/consumer.go`, `kafka.go`, `deadletter.go`.

## 5. Bootstrap rebuild

```mermaid
flowchart LR
  OP(["Operator"])
  RB["cmd/rebuild"]
  OA["offer-availability"]
  PR["pricing"]
  PC["product-content"]
  DB[("PostgreSQL")]
  OP -->|"1 run the job"| RB
  RB -->|"2 qry GET /catalog, then GET /listings"| OA
  RB -->|"3 qry GET /item-prices"| PR
  RB -->|"4 qry GET /product-contents, GET /categories"| PC
  RB -->|"5 fold each row through the same rules"| DB
```

Source: `internal/application/usecases/rebuild.go`, `internal/adapters/outbound/sources/`.
Afterwards the live consumers resume from their committed offsets; the per-source guards drop every replayed
event that is not newer than the snapshot.

## 6. Agent delegation

An agent sends a skill request to `cmd/a2a` (`search_products` or `get_product`). The adapter maps it to the
same `Queries` use case as REST and answers in the reply message; domain rejections are problems inside the
envelope, never JSON-RPC errors. No other context is involved.

Source: `internal/adapters/inbound/a2a/skills.go`.
