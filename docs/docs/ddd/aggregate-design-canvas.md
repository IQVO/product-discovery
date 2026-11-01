---
id: aggregate-design-canvas
title: Aggregate design canvas
sidebar_label: Aggregate design canvas
sidebar_position: 4
---

# Aggregate design canvas

Following the ddd-crew [Aggregate Design Canvas v1.1](https://github.com/ddd-crew/aggregate-design-canvas).

**This context has no aggregate, by decision** ([ADR 0002](/docs/adr/0002-product-discovery-assumptions)): no
command arrives, no state machine decides anything, no invariant is enforced against a caller, and a wrong
projection is rebuilt, not corrected. The canvas is therefore applied to the two **projections** that carry the
context's rules, so a reader can see what a fold may and may not do. The "commands" column is empty; the
"events" column lists consumed facts.

Source: `internal/domain/productview/`, `internal/domain/categoryview/`, `internal/application/usecases/consume*.go`.

## ProductView (one row per catalogue item)

| Section | Content |
| --- | --- |
| **Name** | `ProductView` (`productview.View`), keyed by `catalogItemId` |
| **Description** | The row one product page or search hit renders from. A plain value that facts are folded into; there is no aggregate root and no constructor that enforces a lifecycle |
| **State transitions** | Item: stub (empty) to `ACTIVE` (on `CatalogItemCreated`) to `DISCONTINUED` (on `CatalogItemDiscontinued`, absorbing; a discontinue recorded on a stub is not revived by a late create). Content: none to `PUBLISHED` to `RETIRED`, back to `PUBLISHED` by a higher revision. Price: none to `ACTIVE` to `WITHDRAWN`, back to `ACTIVE` by a higher version. Listing: `LIVE` and `SUPPRESSED`, set by state facts |
| **Commands handled** | None. Facts are folded by `usecases.Consumer` and `Rebuilder` |
| **Events consumed** | `CatalogItemCreated`, `CatalogItemDiscontinued`, `ListingCreated`, `ListingAvailabilityChanged`, `ListingSuppressed`, `ListingRestored`, `ProductContentPublished`, `ProductContentRetired`, `ItemPriceChanged`, `ItemPriceWithdrawn` |
| **Events published** | None |
| **Invariants** | Hold of the row, not rules against a caller: `inStock` and `purchasable` are always recomputed from the row; a listing quantity is never negative; a listing keeps its first item (`ListingItemMap`); listings are ordered by id; ids are 1 to 64 characters of the opaque-id alphabet; the title is 1 to 500 characters and immutable once set |
| **Guards (idempotence and order)** | listing `asOf`, content `revision`, price `version`: a fact applies only when strictly newer, otherwise the fold answers `Stale` and nothing is saved. Inbox on the CloudEvents `id` |
| **Corrective policies** | A fact for an unknown item creates an invisible stub (`productview.Stub`, the model's `StubOnUnknownItem`); a state fact for an unknown listing is skipped; a snapshot never rewinds a known listing state (ADR 0002 A16) |
| **Size and boundary** | One row per item with its listings, bullets, attributes and media as JSONB. Facts of one item are serialised by a transaction-scoped advisory lock (`GetForUpdate`), because three consumer groups write concurrently |
| **Throughput** | Reads and folds scale with the number of items; N>1 pods is supported by the lock and the guards but is not load-tested |
| **Derived state** | `inStock`, `purchasable`, `searchable`, `has_price` and the weighted `tsvector` are stored with the row so search can filter and sort on them |

## CategoryView (the one published tree)

| Section | Content |
| --- | --- |
| **Name** | `CategoryView` (`categoryview.View`), a singleton row |
| **Description** | One published version of the whole category tree. Purchasable counts are not stored: they are derived from ProductView and rolled up the tree at query time |
| **State transitions** | Version 0 (nothing published) to version N to a strictly higher version; a version that is not higher is ignored |
| **Commands handled** | None |
| **Events consumed** | `CategoryTreePublished` |
| **Events published** | None |
| **Invariants** | Ids valid and unique, names non-empty, every parent present, no parent cycle, version at least 1; an invalid tree is an invalid fact (dead-lettered), never half-applied |
| **Guards** | tree `version` strictly higher than the current one; the replacement runs under a row lock (`CurrentForUpdate`) |
| **Size and boundary** | The whole tree in one JSONB value; products keep only a `categoryId`, so a rename or move re-indexes nothing and shows at once on every path |

## Why no aggregate

- There is no command to validate: every state change is a producer's fact, taken as published (Conformist).
- A fold never rejects a well-formed fact; the worst case is `Stale`. The only rejection is an *invalid fact*,
  which is a producer bug and goes to the dead-letter topic.
- Consistency between sources is not enforced here, it is *displayed*: a row with content but no item master
  is invisible, a row with no price is shown without one.

Domain events of the aggregate pattern do not exist; the consumed facts are listed on
[Domain events](/docs/ddd/domain-events), the fold rules on [Use cases](/docs/ddd/use-cases) and the types on
the [Class diagram](/docs/ddd/class-diagram).
