---
id: subdomain-classification
title: Subdomain classification
sidebar_label: Subdomain classification
sidebar_position: 2
---

# Subdomain classification

**Product Discovery is a Supporting subdomain**, Storefront area, CloudEvents tier `storefront`. The verdict
is the one in the storefront subdomain classification of the aggregator
([storefront classification](https://iqvo.github.io/warehouse-docs/storefront/strategic-design/subdomain-classification)):
business model *conversion enabler* (finding the right purchasable item fast), evolution *custom-built*,
aggregates *none (read side only)*, rationale "Core candidate at scale; the engine itself is generic."

## Reasoning

| Question | Answer | Why |
| --- | --- | --- |
| Is it Generic (buy it)? | No | A search engine is generic, and that part is parked behind a port, but the **purchasable join** (item `ACTIVE`, content `PUBLISHED`, price `ACTIVE`, at least one `LIVE` listing with stock) is this storefront's own work in one row that no upstream owns. No product ships that join |
| Is it Core (win with it)? | Not today | No shopper picks this storefront for ranking science: the weights are fixed (title A, category names A, attributes B, description C), there is no learning loop and no analytics event to build one on. The differentiators of the storefront are elsewhere: the price of speed in `pricing`, the promise in the retail network |
| Why "Core candidate at scale"? | Volume, not rules | On the ddd-crew chart the model places the context at 0.62 complexity and 0.55 differentiation, just over both midlines. The complexity is plumbing, not rules: three consumed topics, eleven event types, three per-source monotonic guards, a search index to keep coherent with the projections and a snapshot rebuild. If catalogue and query volume outgrow one PostgreSQL search path, the engine slice is swapped behind the `SearchPort` and the context is reclassified; the join and the projections stay |
| Does it hold domain rules? | Few | No aggregate, no state machine, no domain error. The rules are the fold rules and guards in `internal/domain/productview` and the `PurchasableRule` |

Within the context the slices differ: the *purchasable join* is closer to Core, the *search plumbing*
(`tsvector`, `pg_trgm`, three facets) is generic, and the *inbox and CloudEvents consumers* are fleet
commodity. See the [Core domain chart](/docs/ddd/core-domain-chart).

## Neighbours

Copied from the storefront classification of the aggregator (`storefront/docs/strategic-design/subdomain-classification.md`
on `origin/main`); the neighbours this context has an edge with are marked.

| Bounded context | Classification | Tier | Edge with product-discovery |
| --- | --- | --- | --- |
| `pricing` | Core | `storefront` | upstream: `ItemPriceChanged`, `ItemPriceWithdrawn` (Conformist); `GET /item-prices` on the rebuild |
| `product-content` | Generic | `storefront` | upstream: three content and category-tree events (Conformist); `GET /product-contents` and `GET /categories` on the rebuild |
| `product-discovery` | Supporting | `storefront` | this context |
| `shopping-cart` | Supporting | `storefront` | none (Separate Ways): the cart keeps its own purchasability view from the same feeds |
| `checkout` | Supporting | `storefront` | none |
| `customer-payments` | Generic | `storefront` | none |

| Neighbour outside the storefront | Classification | Edge |
| --- | --- | --- |
| `offer-availability` (retail network) | Supporting | upstream: six item and listing events (Conformist); `GET /catalog` and the ops-only `GET /listings` on the rebuild |
| `storefront-bff` (experience layer) | not a bounded context | the only client of the three REST reads (Open Host Service) |

Counts of the storefront table: three Supporting, two Generic, one Core. The experience layer and the
payment provider simulator carry no domain model and are not classified.

## What would change the verdict

- Catalogue and query volume beyond what PostgreSQL full-text search serves: swap the engine behind
  `SearchPort`, then reclassify.
- Ranking becoming a differentiator (learning to rank, personalisation, sponsored placement): the context
  would move up towards Core and would need the search analytics it does not emit today.
- A bought search product that ships the purchasable join: it would fall to Generic.
