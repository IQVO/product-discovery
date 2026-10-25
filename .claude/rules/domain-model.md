# Domain model: ubiquitous language, projections, events, use cases

## Ubiquitous Language (use these exact names)

- **ProductView** — the projection one product page or search hit renders from; one row per
  catalogue item, folded from facts of three producers. Keyed by `catalogItemId`.
- **CategoryView** — the last published category tree (one `version`) with purchasable counts
  rolled up the tree; product rows keep the `categoryId` only, the path is joined at query time.
- **Stub** — a ProductView row CatalogItemCreated has not completed yet (`IsStub()` is "no title");
  created by a fact that arrived first (rule ErrNone) and invisible to every read. `ItemStub` is the
  empty item state of such a row; a late discontinue leaves it `DISCONTINUED` but still a stub.
- **Purchasable** — item ACTIVE, content PUBLISHED, price ACTIVE and in stock (PurchasableRule).
- **In Stock** — at least one LIVE listing with `availableQty > 0`.
- **Searchable** — item ACTIVE with PUBLISHED content: listed in search (even if unpurchasable).
- **ListingItemMap** — lookup `listingId -> catalogItemId` learned from ListingCreated.
- **Guard** — the per-source monotonic check: listing `asOf`, content `revision`, price `version`,
  tree `version`. Every fold returns an `Outcome`: `Applied`, or `Stale` when the guard refuses the fact
  (acknowledged without effect). A withdrawal or retire at a newer version is still recorded, so a
  replayed older change cannot revive the row.
- **StateKnown** — a listing whose LIVE/SUPPRESSED state some state fact (create, suppress, restore,
  snapshot) set; a snapshot never rewinds a known state (A16).
- **Mode** — `browse`, `fts` or `trgm`: how a page was matched; carried in the `Cursor`.
- **Read DTOs** — `readmodel.Hit`, `SearchResult` and `usecases.Product`, `HitView`, `SearchPage`,
  `CategoryList`: what the queries return; they are shapes of ProductView, not concepts of their own.
- **Price band** — fixed facet edges in minor units (`<2500`, `2500-4999`, ..., `25000+`).
- **Product-type query** — a text query that names a category; the answer adds category suggestions.

## Projections (no aggregate in v1)

- **ProductView** (`internal/domain/productview`): the `Apply...` fold rules (`ApplyItemCreated`,
  `ApplyListingCreated`, `ApplyAvailability`, `ApplyListingState`, `ApplyContentPublished`,
  `ApplyContentRetired`, `ApplyPriceChanged`, `ApplyPriceWithdrawn`, `ApplyItemDiscontinued`,
  `ApplyListingSnapshot` for the rebuild); a
  well-formed fact never errors, a malformed one is `shared.ErrInvalidFact` (dead-lettered).
- **CategoryView** (`internal/domain/categoryview`): `New` (validates ids, names, parents, cycles),
  `Replace` under the version guard, `Get`, `Names`, `Subtree`, `Counted`, `Match`, `Suggestions`.
- **search** (`internal/domain/search`): `Query` (validated, `ErrValidation`), `Cursor`, `Bands`,
  `Facets`, pg_trgm `Similarity`, token `Stem`.
- `internal/domain/shared`: `ErrInvalidFact`, `ValidID` (1 to `MaxID` = 64 characters). Item titles are
  at most `productview.MaxTitle` = 500 characters.

## Domain events

Published: none. Consumed (ADR 0003): CatalogItemCreated, CatalogItemDiscontinued, ListingCreated,
ListingAvailabilityChanged, ListingSuppressed, ListingRestored, ProductContentPublished,
ProductContentRetired, CategoryTreePublished, ItemPriceChanged, ItemPriceWithdrawn.

## Key use cases (`internal/application/usecases`)

- `Queries.SearchProducts` / `GetProduct` / `ListCategories` — read through `SearchPort` and the
  repositories; SearchProducts retries in pg_trgm mode when a first full-text page is empty.
- `Consumer.Apply...` — one per consumed event (ListingSuppressed and ListingRestored share
  `ApplyListingStateChanged`); claim the inbox row and fold in one UnitOfWork.
- `Rebuilder.Run` (`cmd/rebuild`) — pages the five snapshot reads (`Sources`: `CatalogSource`,
  `ListingSource`, `PriceSource`, `ContentSource`) and folds them through the same rules; unfoldable rows
  are counted in `Report.Skipped`.
