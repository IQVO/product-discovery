---
id: sequence-diagrams
title: Sequence diagrams
sidebar_label: Sequence diagrams
sidebar_position: 9
---

# Sequence diagrams

Adapter to store for every use case, including the failure branches. The participants are the real packages;
Postgres is one database. A2A skills follow the same paths as the REST reads they mirror.

## SearchProducts

```mermaid
sequenceDiagram
  autonumber
  actor BFF as storefront-bff
  participant H as inbound http
  participant Q as usecases.Queries
  participant D as domain search
  participant PG as PostgreSQL
  BFF->>H: GET /products?q=ketle&inStock=true
  H->>H: parse parameters, reject NUL
  H->>Q: SearchProducts(input)
  Q->>D: NewQuery(input)
  alt invalid input
    D-->>Q: ValidationError
    Q-->>H: error, metric failed validation-failed
    H-->>BFF: 422 problem validation-failed
  else valid
    Q->>PG: read category view
    PG-->>Q: tree and version
    Q->>PG: full-text search (tsvector, filters, keyset, facets)
    PG-->>Q: hits and facet tallies
    opt first page with zero hits
      Q->>PG: search again in trigram mode
      PG-->>Q: hits and facet tallies
    end
    Q->>Q: join category paths, build suggestions, encode next cursor
    Q-->>H: SearchPage, metric served or empty
    H-->>BFF: 200 hits, facets, categorySuggestions, next
  end
```

Source: `internal/application/usecases/queries.go`, `queries_metrics.go`, `internal/adapters/inbound/http/handlers.go`.

## GetProduct

```mermaid
sequenceDiagram
  autonumber
  actor BFF as storefront-bff
  participant H as inbound http
  participant Q as usecases.Queries
  participant PG as PostgreSQL
  BFF->>H: GET /products/CI-004512
  H->>Q: GetProduct(id)
  Q->>PG: read the products row
  alt no row or stub row
    PG-->>Q: not found or empty title
    Q-->>H: ErrNotFound
    H-->>BFF: 404 problem not-found
  else catalogued
    PG-->>Q: row
    Q->>PG: read category view
    PG-->>Q: tree
    Q-->>H: view and category path
    H-->>BFF: 200 product view
  end
```

Source: `internal/application/usecases/queries.go`, `internal/adapters/inbound/http/handlers.go`.

## ListCategories

```mermaid
sequenceDiagram
  autonumber
  actor BFF as storefront-bff
  participant H as inbound http
  participant Q as usecases.Queries
  participant PG as PostgreSQL
  BFF->>H: GET /categories
  H->>Q: ListCategories()
  Q->>PG: read category view
  PG-->>Q: tree and version, zero view before the first publication
  Q->>PG: count purchasable products per category id
  PG-->>Q: direct counts
  Q->>Q: roll counts up the tree
  Q-->>H: version and counted categories
  H-->>BFF: 200
```

Source: `internal/application/usecases/queries.go` (`ListCategories`).

## Fold of a consumed event

```mermaid
sequenceDiagram
  autonumber
  participant K as Kafka topic
  participant C as inbound kafka consumer
  participant U as usecases.Consumer
  participant D as domain productview
  participant PG as PostgreSQL
  K->>C: fetch one message
  C->>C: decode CloudEvents 1.0, dispatch on full type
  alt not a CloudEvent or unknown type
    C->>K: commit offset, WARN only for a bad envelope
  else known type
    C->>U: Apply...(meta, input) inside a consumer span
    U->>PG: begin unit of work
    U->>PG: claim event id in processed_events
    alt id already seen
      PG-->>U: not first
      U->>PG: commit
    else first time
      U->>PG: lock the row for update (stub when missing)
      U->>D: fold rule
      D-->>U: next view and Applied or Stale
      opt Applied
        U->>PG: save row, derived flags and search document
      end
      U->>PG: commit
    end
    U-->>C: nil
    C->>K: commit offset
  end
```

Source: `internal/adapters/inbound/kafka/{consumer,tracing,kafka}.go`, `internal/application/usecases/consume.go`.
A serialization failure re-runs the unit of work up to 5 times.

## Failure: retry and dead letter

```mermaid
sequenceDiagram
  autonumber
  participant K as Kafka topic
  participant C as consume loop
  participant U as usecases.Consumer
  participant DLQ as topic dlq
  K->>C: fetch message
  loop up to 5 attempts, backoff 200 ms doubling to 5 s
    C->>U: handle
    U-->>C: transient error (database)
  end
  C->>DLQ: publish with x-dlq headers, RequireAll acks
  alt publish fails
    C->>DLQ: retry until it succeeds, partition stays blocked
  end
  C->>K: commit offset
  Note over C,DLQ: an undecodable or unusable fact skips the retries and goes straight to the dead letter
```

Source: `internal/adapters/inbound/kafka/kafka.go` (`handleWithDeadLetter`, `retryUntilOK`), `deadletter.go`.

## RebuildProjections

```mermaid
sequenceDiagram
  autonumber
  actor Op as operator
  participant R as cmd rebuild
  participant PG as PostgreSQL
  participant OA as offer-availability
  participant PR as pricing
  participant PC as product-content
  Op->>R: run the Job
  R->>PG: run migrations (bounded retry)
  loop catalogue pages of 500
    R->>OA: GET /catalog?after=
    OA-->>R: page
    R->>PG: fold each row in its own unit of work
  end
  loop listing pages
    R->>OA: GET /listings?after=
    R->>PG: learn mapping and fold snapshot rows
  end
  loop price pages
    R->>PR: GET /item-prices?after=
    R->>PG: fold each row
  end
  loop content pages
    R->>PC: GET /product-contents?after=
    R->>PG: fold each row, skipping DRAFT
  end
  R->>PC: GET /categories
  R->>PG: replace the tree if its version is higher
  R-->>Op: rebuild finished with counters
```

Source: `internal/application/usecases/rebuild.go`, `cmd/rebuild/main.go`. A page that fails after 4 attempts
aborts the run (`rebuild failed`, exit 1).

## A2A skill call

```mermaid
sequenceDiagram
  autonumber
  actor AG as delegating agent
  participant A as cmd a2a router
  participant S as a2a executor
  participant Q as usecases.Queries
  participant PG as PostgreSQL
  AG->>A: GET /.well-known/agent-card.json
  A-->>AG: Agent Card with skills
  AG->>A: JSON-RPC message with skill and arguments
  A->>S: executor
  S->>S: parse first part, strict decode
  alt malformed request or unknown skill
    S-->>AG: reply with problem 400
  else known skill
    S->>Q: SearchProducts or GetProduct inside an a2a.skill span
    Q->>PG: same reads as REST
    PG-->>Q: rows
    Q-->>S: page or product, or an error
    S-->>AG: reply with data, or a problem 404 422 500
  end
```

Source: `internal/adapters/inbound/a2a/{server,skills}.go`, `cmd/a2a/router.go`.
