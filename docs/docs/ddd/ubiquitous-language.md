---
id: ubiquitous-language
title: Ubiquitous language
sidebar_label: Ubiquitous language
sidebar_position: 1
---

# Ubiquitous language

The terms `product-discovery` speaks, each with the identifier that implements it. The first table is the
context's own vocabulary, the second the neighbour terms it projects but does not own (their definitions
belong to the owning context), the third terms that are easy to confuse. The storefront-wide glossary is on
the [aggregator site](https://iqvo.github.io/warehouse-docs/storefront/strategic-design/ubiquitous-language).

Source: `internal/domain/{productview,categoryview,search,shared}`, `internal/application/{ports,usecases}`.

## Own terms

| Term | Definition | Code identifier | Not to be confused with |
| --- | --- | --- | --- |
| **Product View** | The projection one product page or search hit renders from: one row per catalogue item, folded from facts | `productview.View` | The catalogue item (`offer-availability`) or its content (`product-content`) |
| **Category View** | The browsable tree of one published version, with purchasable counts per category | `categoryview.View`, `categoryview.Counted` | The category tree aggregate in `product-content`, which owns the tree; this view adds counts |
| **Purchasable** | Item `ACTIVE`, content `PUBLISHED`, price `ACTIVE`, and at least one `LIVE` listing with `availableQty` above 0 | `View.Purchasable` | The network's sellable listing (one node): purchasable is an item-level verdict this context derives |
| **In Stock** | At least one `LIVE` listing with `availableQty` above 0 | `View.InStock` | Purchasable: stock is one of its four inputs |
| **Searchable** | Item `ACTIVE` and content `PUBLISHED`: the row is offered in search and browse, even when unpurchasable | `View.Searchable` | Purchasable |
| **Stub** | The invisible row an out-of-order fact creates before `CatalogItemCreated` has completed it; `IsStub` is true while the title is empty | `productview.Stub`, `View.IsStub` | A deleted or draft product |
| **Fold** | Applying one fact to a row under a per-source guard; answers `Applied` or `Stale` | `View.Apply...`, `productview.Outcome` | A command: a fold never rejects a well-formed fact |
| **Monotonic guard** | Per-source ordering: listing `asOf`, content `revision`, price `version`, tree `version` must be strictly newer; an older fact is `Stale` | the comparisons in `fold_listing.go`, `fold_content.go`, `fold_price.go`, `categoryview.Replace` | The inbox: the inbox deduplicates by id, the guard orders by source sequence |
| **Inbox** | The claim on the CloudEvents `id`, in the same transaction as the fold | `ports.Inbox`, table `processed_events` | The guard |
| **Listing item map** | Read-side lookup `listingId` to `catalogItemId`, learned from listing events | `ports.ListingItemMap`, table `listing_items` | The listing itself |
| **Product-type query** | A search for a kind of product; the answer adds category suggestions (categories whose name covers every query word, shallowest first, at most 5) | `categoryview.View.Suggestions`, `usecases.MaxSuggestions` | A navigational query (a category name) or an item-specific one (brand and model) |
| **Browse** | A query with no free text: a plain filtered listing | `search.Query.Browse` | Search |
| **Typo fallback** | The `pg_trgm` similarity match used when a first full-text page finds nothing; similarity at least 0.3 | `search.SimilarityThreshold`, `search.ModeTrgm` | Synonym or stem expansion |
| **Mode** | The matching strategy of a page: `browse`, `fts` or `trgm`; recorded in the cursor | `search.Mode` | Sort |
| **Cursor** | The opaque keyset position after the last hit (base64url JSON), bound to a sort and a mode | `search.Cursor` | An offset |
| **Facet** | A filter axis with counts over the whole filtered result: `category`, `price-band`, `in-stock` | `search.Facet`, `search.NewFacets` | Free-form filters |
| **Price band** | A range of effective price in minor units, lower edge included; edges from `PRICE_BAND_EDGES`, default 2500, 5000, 10000, 25000 | `search.Band`, `search.Bands` | A price filter (`priceMin`, `priceMax`) |
| **Search port** | The port search sits behind: PostgreSQL full-text search plus `pg_trgm` today, a dedicated engine later | `ports.SearchPort` | A ranking service: the weights are fixed |
| **Snapshot rebuild** | Seeding the projections from the producers' paged reads, through the same fold rules; ops-run, never on a user request | `usecases.Rebuilder`, `cmd/rebuild` | A user-request read |
| **Dead letter** | A message copied to `<topic>.dlq` with `x-dlq-*` headers because it can never be applied or exhausted its attempts | `DLQSuffix` and `publishDeadLetter` (`internal/adapters/inbound/kafka/deadletter.go`) | A skipped message (not a CloudEvent, unknown type) |
| **Invalid fact** | A consumed or snapshot fact that can never be folded (bad id, negative quantity, bad price) | `shared.ErrInvalidFact` | A stale fact |
| **Skill** | One A2A capability: `search_products` or `get_product` | `skills` in `internal/adapters/inbound/a2a` | An MCP tool (there is none) |

## Neighbour terms this context projects

| Term | Owner's definition | Owner |
| --- | --- | --- |
| **Catalog item** | The network's own entry for a product, independent of who ships it | `offer-availability` |
| **Listing** | A catalogue item offered from one node; states `LIVE` and `SUPPRESSED` | `offer-availability` |
| **Available quantity** | What a node claims it can ship now for one listing, with the `asOf` of the claim | `offer-availability` |
| **Effective price** | The lowest active sale price, else the list price | `pricing` |
| **Product content** | Description, bullets, attributes and media of one catalogue item; one **revision** per publication | `product-content` |
| **Category tree** | The single, versioned tree of categories | `product-content` |

## Terms to keep apart

| Term | Is | Is not |
| --- | --- | --- |
| `ACTIVE` / `DISCONTINUED` | The item master states of the whole item | The listing states `LIVE` / `SUPPRESSED` |
| `PUBLISHED` / `RETIRED` | The content state this row knows | A producer's `DRAFT` (the rebuild skips drafts) |
| `ACTIVE` / `WITHDRAWN` | The price state this row knows | A zero price: a withdrawn price clears the amounts |
| Stale | A fact the guard dropped; acknowledged, not an error | Invalid: a fact that can never be folded, dead-lettered |
| Unavailable | A row that is not purchasable, still shown as currently unavailable | Not found: a stub or unknown id is a 404 |
