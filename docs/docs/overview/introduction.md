---
id: introduction
title: Introduction
sidebar_label: Introduction
sidebar_position: 1
---

# Product Discovery

`product-discovery` is a **Storefront-area Supporting** bounded context of the warehouse-systems fleet. It is
the read side of browsing and search: it keeps one **ProductView** per catalogue item (title, flags, content,
category path, list and effective price, `inStock`, `purchasable`) and one **CategoryView** (the browsable
category tree plus purchasable counts), and it answers three questions for the shopper's storefront: which
products match what I typed or filtered, what does this product page look like, and which categories can I
browse. Both views are projections. The context owns no aggregate, takes no command, serves no write route and
**publishes no event** ([ADR 0001](/docs/adr/0001-product-discovery-bounded-context),
[ADR 0002](/docs/adr/0002-product-discovery-assumptions)).

The facts come from three producers over Kafka: the retail network's `offer-availability` (item master and
listing availability), the storefront's `product-content` (content and the category tree) and `pricing`
(prices). The context joins them into the single answer a shopper sees, above all the **purchasable** verdict
(item active, content published, price active, at least one live listing with stock), which none of the three
producers can give alone. A product that cannot be bought is still shown, as currently unavailable.

:::info What runs on this branch
Three binaries: `cmd/api` (REST on `:8080` plus three always-on Kafka consumers), `cmd/a2a` (a read-only
Agent2Agent agent on `:8093`, [ADR 0007](/docs/adr/0007-a2a-inbound-adapter)) and `cmd/rebuild` (a one-shot
snapshot backfill, shipped as an optional, default-off Helm Job,
[ADR 0006](/docs/adr/0006-rebuild-as-default-off-helm-job)). It runs on real Postgres and real Kafka only:
there is no in-memory mode, and `DATABASE_URL`, `KAFKA_BROKERS` and the three consumer group ids are
mandatory. There is no MCP server. REST and A2A are unauthenticated fleet-wide. Packaging is the Helm chart
`charts/product-discovery` and one image carrying all three binaries.
:::

## What the context owns

| Concept | Where it lives | Rule in one line |
| --- | --- | --- |
| **ProductView** (one row per catalogue item) | `internal/domain/productview` | A value that facts are folded into, guarded per source: listing `asOf`, content `revision`, price `version`, all strictly monotonic. `inStock` and `purchasable` are recomputed on every fold. |
| **Stub row** | same | A fact for an item the item master has not announced creates an invisible row; `CatalogItemCreated` completes it (ADR 0002). |
| **PurchasableRule** | `View.Purchasable` | Item `ACTIVE`, content `PUBLISHED`, price `ACTIVE` and at least one `LIVE` listing with `availableQty > 0`. |
| **CategoryView** (one tree) | `internal/domain/categoryview` | One published tree version, replaced wholesale when its `version` is strictly higher; purchasable counts are derived from ProductView and roll up the tree. |
| **Search** | `internal/domain/search` | Validated `Query`, keyset `Cursor`, price `Bands`, `Facets`, trigram similarity. Run by PostgreSQL full-text search with a `pg_trgm` typo fallback behind the `SearchPort`. |
| **ListingItemMap** | `listing_items` table | The read-side lookup `listingId` to `catalogItemId`, learned from `ListingCreated` and `ListingAvailabilityChanged`, used by `ListingSuppressed` and `ListingRestored`. |
| **Inbox** | `processed_events` table | Idempotency on the CloudEvents `id`, written in the same transaction as the fold. |

## What it does not own

| Out of scope | Belongs to |
| --- | --- |
| The item master (title, dimensions, flags) and listing availability as facts | `offer-availability` (retail network) |
| Descriptions, media, attributes and the category tree itself | `product-content` |
| List prices, sale windows and the effective price | `pricing` |
| The cart, the checkout, payment | `shopping-cart`, `checkout`, `customer-payments` |
| Orders, reservations, delivery promises | the retail network |
| Recommendations, reviews, personalisation, sponsored placement | later phases; ranking weights are fixed (title A, category names A, attributes B, description C) |
| Any write path, any published event, any analytics of what was searched | not in this context |
| Authentication | removed fleet-wide; there is none to configure |

## Classification

- **Supporting subdomain**, Storefront area, CloudEvents tier `storefront`. The search engine itself is
  generic (PostgreSQL behind a port) and the competitive differentiation is the purchasable join, which is
  custom-built but not where the business wins; it is a Core candidate at scale
  ([Subdomain classification](/docs/ddd/subdomain-classification)).

## Main capabilities

1. **Search** products (`GET /products`): full-text over title, category names, attributes and description,
   `pg_trgm` typo fallback on a first page that finds nothing, filters ANDed (`categoryId` subtree, `inStock`,
   `priceMin`, `priceMax`), four sorts, keyset paging, three facets (`category`, `price-band`, `in-stock`) and
   category suggestions for product-type queries.
2. **Read one product page** (`GET /products/{catalogItemId}`): the full ProductView with the category path
   joined at query time.
3. **Browse the category tree** (`GET /categories`) with purchasable counts per category.
4. **Fold** eleven CloudEvents types from three topics into the projections, at least once with an atomic
   effect, idempotent on the event id and safe under replay and out-of-order delivery; unusable messages go
   to `<topic>.dlq`.
5. **Rebuild** the projections from the producers' paged snapshot reads (`cmd/rebuild`), safe to run beside
   the live consumers.
6. **Delegate to agents** over A2A: the skills `search_products` and `get_product` answer from the same use
   cases as REST.

## Where to go next

| You want to | Read |
| --- | --- |
| See the layers, packages, binaries and the rules that police them | [Architecture](/docs/overview/architecture) |
| Run it and make the first calls | [Quickstart](/docs/overview/quickstart) |
| Know every setting | [Configuration](/docs/operations/configuration) |
| Deploy and operate it | [Runbook](/docs/operations/runbook), [Observability](/docs/operations/observability), [Before the first release](/docs/operations/before-first-release) |
| Fix something that broke | [Troubleshooting](/docs/operations/troubleshooting) |
| Know what tests and CI gates exist | [Testing](/docs/development/testing) |
| See who talks to whom | [Integration](/docs/ecosystem/integration) |
| See the use cases and their invariants | [Use cases](/docs/ddd/use-cases) |
| Read the design as ddd-crew artifacts | [DDD artifacts](/docs/ddd/ddd-artifacts), [Ubiquitous language](/docs/ddd/ubiquitous-language) |
| Read the contracts | [API overview](/docs/api-reference), [Event catalogue](/docs/api-reference/events) |
| Read the decisions | [ADR index](/docs/adr) |

Published at [https://iqvo.github.io/product-discovery/](https://iqvo.github.io/product-discovery/).
