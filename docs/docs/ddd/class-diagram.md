---
id: class-diagram
title: Class diagram
sidebar_label: Class diagram
sidebar_position: 7
---

# Class diagram

UML class diagrams of the read-side model and of the hexagonal ports and the adapters that implement them.
Go has no classes: a box is a struct or an interface, a method is a function with that receiver. Only the
types and members that carry meaning are drawn.

## Domain: ProductView and its value types

```mermaid
classDiagram
  class View {
    +CatalogItemID string
    +Title string
    +Item ItemState
    +Content ContentState
    +ContentRevision int64
    +Price PriceState
    +PriceVersion int64
    +Currency string
    +ListPriceMinor int64
    +EffectivePriceMinor int64
    +CategoryID string
    +InStock() bool
    +Purchasable() bool
    +Searchable() bool
    +HasPrice() bool
    +IsStub() bool
    +ApplyItemCreated(ItemCreated) View, Outcome, error
    +ApplyItemDiscontinued() View, Outcome
    +ApplyListingCreated(ListingCreated) View, Outcome, error
    +ApplyAvailability(AvailabilityChanged) View, Outcome, error
    +ApplyListingState(string, ListingState) View, Outcome
    +ApplyListingSnapshot(ListingSnapshot) View, Outcome, error
    +ApplyContentPublished(PublishedContent) View, Outcome, error
    +ApplyContentRetired(int64) View, Outcome, error
    +ApplyPriceChanged(PriceChanged) View, Outcome, error
    +ApplyPriceWithdrawn(int64) View, Outcome, error
  }
  class Listing {
    +ListingID string
    +NodeID string
    +State ListingState
    +AvailableQty int64
    +AsOf time
    +StateKnown bool
  }
  class Attribute {
    +Name string
    +Value string
    +Unit string
  }
  class Media {
    +MediaID string
    +URL string
    +AltText string
    +Role string
  }
  class Outcome {
    <<enumeration>>
    Applied
    Stale
  }
  class ItemState {
    <<enumeration>>
    Stub
    ACTIVE
    DISCONTINUED
  }
  class ContentState {
    <<enumeration>>
    None
    PUBLISHED
    RETIRED
  }
  class PriceState {
    <<enumeration>>
    None
    ACTIVE
    WITHDRAWN
  }
  View "1" *-- "0..*" Listing : listings
  View "1" *-- "0..*" Attribute
  View "1" *-- "0..*" Media
  View ..> Outcome
  View --> ItemState
  View --> ContentState
  View --> PriceState
```

Source: `internal/domain/productview/view.go`, `fold_item.go`, `fold_listing.go`, `fold_content.go`,
`fold_price.go`.

## Domain: CategoryView and search

```mermaid
classDiagram
  class CategoryTree {
    +Version() int64
    +Categories() Category list
    +Get(id) Category, bool
    +Names(id) string list
    +Subtree(id) string list
    +Counted(direct) Counted list
    +Match(text) string list
    +Suggestions(text, limit) Category list
  }
  class Category {
    +ID string
    +ParentID string
    +Name string
    +Path string
  }
  class Counted {
    +PurchasableCount int
  }
  class Query {
    +Text string
    +CategoryID string
    +InStock bool ptr
    +PriceMin int64 ptr
    +PriceMax int64 ptr
    +Sort Sort
    +After Cursor ptr
    +Limit int
    +Browse() bool
  }
  class Cursor {
    +Sort Sort
    +Mode Mode
    +Rank float64
    +Price int64
    +Title string
    +ID string
    +Encode() string
  }
  class Band {
    +Lower int64
    +Upper int64
    +Label() string
  }
  class Facet {
    +Name string
    +Values FacetValue list
  }
  CategoryTree "1" *-- "0..*" Category
  Counted --|> Category
  Query --> Cursor
  Facet ..> Band
```

Source: `internal/domain/categoryview/categoryview.go`, `internal/domain/search/{query,cursor,bands,facets}.go`.
`CategoryTree` is `categoryview.View`, renamed here to avoid a clash with `productview.View`.

## Application: ports and use cases

```mermaid
classDiagram
  class Queries {
    +GetProduct(id) Product, error
    +ListCategories() CategoryList, error
    +SearchProducts(Input) SearchPage, error
  }
  class Consumer {
    +ApplyCatalogItemCreated()
    +ApplyCatalogItemDiscontinued()
    +ApplyListingCreated()
    +ApplyListingAvailabilityChanged()
    +ApplyListingStateChanged()
    +ApplyProductContentPublished()
    +ApplyProductContentRetired()
    +ApplyCategoryTreePublished()
    +ApplyItemPriceChanged()
    +ApplyItemPriceWithdrawn()
  }
  class Rebuilder {
    +Run() Report, error
  }
  class Deps
  class ProductRepository {
    <<interface>>
    Get() GetForUpdate() Save() PurchasableByCategory()
  }
  class CategoryRepository {
    <<interface>>
    Current() CurrentForUpdate() Replace()
  }
  class ListingItemMap {
    <<interface>>
    Get() Put()
  }
  class Inbox {
    <<interface>>
    Claim()
  }
  class UnitOfWork {
    <<interface>>
    Do()
  }
  class SearchPort {
    <<interface>>
    Search()
  }
  class SearchMetrics {
    <<interface>>
    SearchServed() SearchEmpty() SearchFailed()
  }
  class Sources {
    <<interface>>
    ListCatalog() ListListings() ListItemPrices() ListProductContents() ListCategories()
  }
  Queries o-- Deps
  Consumer o-- Deps
  Rebuilder --|> Consumer
  Rebuilder o-- Sources
  Deps o-- ProductRepository
  Deps o-- CategoryRepository
  Deps o-- ListingItemMap
  Deps o-- Inbox
  Deps o-- UnitOfWork
  Deps o-- SearchPort
  Deps o-- SearchMetrics
```

Source: `internal/application/usecases/*.go`, `internal/application/ports/ports.go`.
`Sources` groups the four snapshot ports (`CatalogSource`, `ListingSource`, `PriceSource`, `ContentSource`).

## Adapters

```mermaid
classDiagram
  class ProductRepository {
    <<interface>>
  }
  class SearchPort {
    <<interface>>
  }
  class Inbox {
    <<interface>>
  }
  class UnitOfWork {
    <<interface>>
  }
  class PostgresAdapters {
    ProductRepository CategoryRepository ListingItemMap Inbox UnitOfWork Search
  }
  class MemoryStore {
    test double only
  }
  class RestServer {
    chi router, problems, readiness
  }
  class A2AExecutor {
    Agent Card, skills
  }
  class KafkaConsumer {
    offer-availability product-content pricing
  }
  class SnapshotSources {
    OfferAvailability Pricing ProductContent
  }
  class SearchMetricsAdapter {
    discovery.searches.served
  }
  PostgresAdapters ..|> ProductRepository
  PostgresAdapters ..|> SearchPort
  PostgresAdapters ..|> Inbox
  PostgresAdapters ..|> UnitOfWork
  MemoryStore ..|> ProductRepository
  MemoryStore ..|> SearchPort
  RestServer --> Queries
  A2AExecutor --> Queries
  KafkaConsumer --> Consumer
  SnapshotSources ..|> Sources
  class Queries
  class Consumer
  class Sources
```

Source: `internal/adapters/outbound/postgres/`, `internal/adapters/outbound/memory/`,
`internal/adapters/inbound/{http,a2a,kafka}/`, `internal/adapters/outbound/{sources,telemetry}/`. The memory
adapters are shared by the unit tests and the godog suite; `TestRuntimeBinaryHasNoInMemoryAdapters` keeps them
out of the binaries. One contract suite (`internal/adapters/outbound/repocontract`) runs against both the
memory store and Postgres.
