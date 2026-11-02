---
id: quickstart
title: Quickstart
sidebar_label: Quickstart
sidebar_position: 3
---

# Quickstart

Run `product-discovery` on your machine, feed it a few events and search the result. The service is a
read model: it has no write API, so the only way data gets in is a CloudEvent on one of the three
consumed topics (or the `cmd/rebuild` snapshot job).

:::note What was and was not run
This page was written from the code and the specs (`Makefile`, `cmd/*/main.go`, `apis/*.yaml`). The
commands and the response shapes are taken from those files; the page does not contain captured
terminal output. Response bodies below are shown as shapes, not as recorded runs.
:::

## Prerequisites

| Tool | Why |
| --- | --- |
| Go (version in `go.mod`) | build and run the binaries |
| A Postgres you can create a database in (with the `pg_trgm` extension available) | the read model; there is no in-memory mode |
| A Kafka broker | the three consumers, the dead letters and `/readyz`; in the fleet there is exactly **one** broker, reachable from the host at `localhost:9092` when the kind cluster from `warehouse-infra` is up. The old docker-compose Kafka is retired |
| Docker | only for `make integration` (testcontainers boots its own Postgres and Kafka) and for a throwaway local Postgres |
| `kcat` (optional) | to publish sample events by hand |
| `lefthook` (optional) | git hooks: pre-commit `fmt-check` + `vet` + `lint`, pre-push `make check` |

The repository ships no compose file. Point the variables at any Postgres and Kafka.

## Make targets

`make help` lists them all. Every target mirrors a CI job.

| Target | What it does |
| --- | --- |
| `make build` | `go build ./...` |
| `make test` | `go test ./... -race` (unit, httptest and godog; no database needed) |
| `make check` | `fmt-check`, `vet`, `build`, `lint`, `test` (what pre-push runs) |
| `make check-all` | `check` plus `coverage` (90 % gate over `internal/domain` and `internal/application`), `arch-test`, `bdd` |
| `make check-fast` | `fmt-check`, `vet`, `arch-test` and the tests of packages changed vs HEAD |
| `make integration` | `go test -tags=integration ./... -race -count=1`; Docker only |
| `make mutation` / `make mutation-full` | gremlins over every `internal/domain/<pkg>` / the exhaustive run |
| `make vuln` | `govulncheck ./...` |

See [Testing](/docs/development/testing) for what each layer proves.

## 1. A database

If you do not have one, a throwaway Postgres:

```sh
docker run -d --name pd-pg -e POSTGRES_USER=pd -e POSTGRES_PASSWORD=pd \
  -e POSTGRES_DB=product_discovery -p 5432:5432 postgres:16
```

The migrations are embedded in the binary and run at boot (`internal/adapters/outbound/postgres/migrate.go`).
They create `products`, `category_view`, `listing_items` and `processed_events`, plus the search indexes.

## 2. Start the API and the consumers

```sh
export DATABASE_URL="postgres://pd:pd@localhost:5432/product_discovery?sslmode=disable"
export KAFKA_BROKERS="localhost:9092"
export OFFER_AVAILABILITY_CONSUMER_GROUP=product-discovery-offer-availability
export PRODUCT_CONTENT_CONSUMER_GROUP=product-discovery-product-content
export PRICING_CONSUMER_GROUP=product-discovery-pricing
go run ./cmd/api
```

A missing variable is a boot error that names it ([Configuration](/docs/operations/configuration)).
Without a reachable Kafka the process still boots (the readers dial lazily) but `/readyz` answers 503
`not ready: kafka`.

```sh
curl -s localhost:8080/healthz     # liveness: always 200 while the process runs
curl -s localhost:8080/readyz      # 200 {"status":"ready"} only when Postgres and a Kafka broker answer
```

## 3. Feed it events

Publish CloudEvents in structured mode (the full envelope in the message value, key = the
`subject`). Two things must be true before an item is purchasable and visible in search: the item
master announced it (`CatalogItemCreated`), content is published, a price is active and at least one
live listing has quantity. The examples are from `apis/asyncapi.yaml`.

```sh
H='content-type=application/cloudevents+json; charset=UTF-8'

echo '{"specversion":"1.0","id":"0c1a4e2b-6d3f-4a8e-9b71-2f5d8c9e0a11","source":"/retailnetwork/offer-availability","type":"com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated","subject":"CI-004512","time":"2026-10-22T09:00:00Z","datacontenttype":"application/json","dataschema":"urn:retailnetwork:offer-availability:events:CatalogItemCreated:v1","data":{"catalogItemId":"CI-004512","title":"Steel kettle 1.7 l","hazmat":false,"fragile":false}}' \
  | kcat -P -b localhost:9092 -t retailnetwork.offer-availability.events -k CI-004512 -H "$H"

echo '{"specversion":"1.0","id":"4a8c2d90-7e15-4b3c-b6f2-0c9d8e7a1b35","source":"/retailnetwork/product-content","type":"com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished","subject":"main","time":"2026-10-22T10:00:00Z","datacontenttype":"application/json","dataschema":"urn:retailnetwork:product-content:events:CategoryTreePublished:v1","data":{"version":5,"categories":[{"categoryId":"cat-home","name":"Home","path":"Home"},{"categoryId":"cat-kettles","parentId":"cat-home","name":"Kettles","path":"Home/Kettles"}]}}' \
  | kcat -P -b localhost:9092 -t retailnetwork.product-content.events -k main -H "$H"

echo '{"specversion":"1.0","id":"7d2e9c41-5f6b-4a3e-9c2d-1e8f0a5b6c7d","source":"/retailnetwork/product-content","type":"com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished","subject":"CI-004512","time":"2026-10-22T14:03:11Z","datacontenttype":"application/json","dataschema":"urn:retailnetwork:product-content:events:ProductContentPublished:v1","data":{"catalogItemId":"CI-004512","revision":2,"description":"A 1.7 litre stainless steel kettle.","bullets":["Auto shut-off"],"attributes":[{"name":"capacity","value":"1.7","unit":"l"}],"media":[{"mediaId":"med-01","url":"https://static.example.invalid/CI-004512/main.jpg","altText":"Kettle, side view","role":"PRIMARY"}],"primaryCategoryId":"cat-kettles"}}' \
  | kcat -P -b localhost:9092 -t retailnetwork.product-content.events -k CI-004512 -H "$H"

echo '{"specversion":"1.0","id":"7a2c9e41-5f0b-4c3d-9e6a-1b8d4f0c2a55","source":"/retailnetwork/pricing","type":"com.retailnetwork.storefront.pricing.itemprice.ItemPriceChanged","subject":"CI-004512","time":"2026-10-24T09:00:00Z","datacontenttype":"application/json","dataschema":"urn:retailnetwork:pricing:events:ItemPriceChanged:v1","data":{"catalogItemId":"CI-004512","currency":"BRL","listPriceMinor":12990,"effectivePriceMinor":9990,"version":7}}' \
  | kcat -P -b localhost:9092 -t retailnetwork.pricing.events -k CI-004512 -H "$H"

echo '{"specversion":"1.0","id":"5b0f8a52-3c1e-4d7a-9f4e-2a6d1c9e7b31","source":"/retailnetwork/offer-availability","type":"com.retailnetwork.commerce.offer-availability.listing.ListingAvailabilityChanged","subject":"LST-000184","time":"2026-10-22T14:05:12Z","datacontenttype":"application/json","dataschema":"urn:retailnetwork:offer-availability:events:ListingAvailabilityChanged:v1","data":{"listingId":"LST-000184","catalogItemId":"CI-004512","nodeId":"NODE-VWS-FC01","availableQty":120,"asOf":"2026-10-22T14:00:00Z"}}' \
  | kcat -P -b localhost:9092 -t retailnetwork.offer-availability.events -k LST-000184 -H "$H"
```

`ListingAvailabilityChanged` for a listing the service has not seen yet adds it LIVE, so a separate
`ListingCreated` is optional for this walkthrough. The events can arrive in any order across topics:
a fact for an item the item master has not announced creates an invisible stub row that
`CatalogItemCreated` completes ([ADR 0002](/docs/adr/0002-product-discovery-assumptions)).

The consumers log each message (`consumer running` at start, a WARN for anything skipped); a message
that cannot be applied lands on `<topic>.dlq` ([Troubleshooting](/docs/operations/troubleshooting)).

## 4. Query it

```sh
# full-text search with facets
curl -s 'localhost:8080/products?q=kettle' | jq .

# browse with filters: in stock, price up to 10000 minor units, cheapest first
curl -s 'localhost:8080/products?inStock=true&priceMax=10000&sort=price-asc&limit=10' | jq .

# one product page
curl -s localhost:8080/products/CI-004512 | jq .

# the category tree with purchasable counts
curl -s localhost:8080/categories | jq .
```

What to expect, from `apis/openapi.yaml` and the code:

- `GET /products` answers `200` with `{"items":[...],"facets":[...],"categorySuggestions":[...]}` plus `nextCursor` when more pages exist.
  Each hit has `catalogItemId`, `title`, `effectivePrice`, `primaryMedia`, `categoryId`, `categoryPath`,
  `inStock`, `purchasable`. The three facets are `category`, `price-band` (values such as `<2500`,
  `2500-4999`, `25000+`; edges from `PRICE_BAND_EDGES`) and `in-stock`. `nextCursor` is an opaque cursor
  for `?cursor=`; a retired `?after=` is a `400`.
- A typo (`?q=ketle`) falls back to `pg_trgm` similarity.
- `GET /products/CI-004512` answers `200` with the full ProductView; an unknown id, or an item that is
  still a stub, answers `404` `application/problem+json` (`type` `/problems/not-found`).
- A bad filter (`?sort=cheap`, `?priceMin=9&priceMax=1`) answers `422`
  `application/problem+json` (`/problems/validation-failed`, `detail` names the field); a bad `limit`
  (`?limit=501`), a bad `cursor` or an undeclared parameter answers `400` (`invalid-limit`, `invalid-cursor`,
  `unknown-query-parameter`).

## 5. Optional: the A2A agent

```sh
export A2A_AGENT_URL=http://localhost:8093/
go run ./cmd/a2a      # DATABASE_URL as above; no Kafka needed
curl -s localhost:8093/.well-known/agent-card.json | jq .
```

The agent serves the same two reads as skills (`search_products`, `get_product`); see the
[API overview](/docs/api-reference).

## 6. Optional: seed from snapshots instead of events

When the producers are running and you want the projections filled without replaying Kafka, run the
rebuild job with the four required variables ([Runbook](/docs/operations/runbook#running-the-rebuild)):

```sh
export OFFER_AVAILABILITY_URL=http://localhost:8081 PRICING_URL=http://localhost:8082 PRODUCT_CONTENT_URL=http://localhost:8083
go run ./cmd/rebuild
```

The ports are placeholders for wherever your producers listen.

## Next

- [Architecture](/docs/overview/architecture) for how the pieces fit.
- [Configuration](/docs/operations/configuration) for every variable.
- [Testing](/docs/development/testing) for the quality gate.
