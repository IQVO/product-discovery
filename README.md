# product-discovery

[![CI](https://github.com/IQVO/product-discovery/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/IQVO/product-discovery/actions/workflows/ci.yml?query=branch%3Adevelop) [![coverage gate](https://img.shields.io/badge/coverage%20gate-90%25-brightgreen)](https://github.com/IQVO/product-discovery/blob/develop/Makefile) [![Go](https://img.shields.io/badge/go-1.27.3-00ADD8?logo=go&logoColor=white)](https://github.com/IQVO/product-discovery/blob/develop/go.mod) [![release](https://img.shields.io/github/v/release/IQVO/product-discovery?sort=semver)](https://github.com/IQVO/product-discovery/releases) [![docs](https://img.shields.io/badge/docs-iqvo.github.io-blue)](https://iqvo.github.io/product-discovery/) [![license](https://img.shields.io/badge/license-MIT-blue)](https://github.com/IQVO/product-discovery/blob/develop/LICENSE)

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Product Discovery** (Supporting, storefront tier).

The read side of browsing and search: one ProductView per catalogue item (title, flags, content, category path, list and effective price, inStock, purchasable) and one CategoryView (tree plus purchasable counts), fed by events from offer-availability, product-content and pricing, searched with PostgreSQL full-text search and pg_trgm (D9), behind a port so a dedicated engine is a later adapter swap.

Status: built (read side, no aggregate). This repository is part of the storefront domain: it consumes the retail network's Open Host Service routes and its published `retailnetwork.*` events; it never calls an in-org or restricted network route.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

## Documentation

This repository publishes its own documentation site at
**[https://iqvo.github.io/product-discovery/](https://iqvo.github.io/product-discovery/)**, built from
[`docs/`](docs/) with Docusaurus (`.github/workflows/docs.yml` builds it on every pull request and deploys it
on push to `main`). The context's design also lives on the aggregator site,
[warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/contexts/product-discovery), where
`storefront/model/model.yaml` is the model of record; the pages below are checked against this code.

Page map (paths relative to `https://iqvo.github.io/product-discovery/docs/`; sources under `docs/docs/`):

| Area | Pages |
|---|---|
| Overview | [introduction](https://iqvo.github.io/product-discovery/docs/overview/introduction) (what the context owns and does not own), [architecture](https://iqvo.github.io/product-discovery/docs/overview/architecture) (layers, the three binaries, data stores), [quickstart](https://iqvo.github.io/product-discovery/docs/overview/quickstart) (prerequisites, `make` targets, local Postgres and Kafka, first calls) |
| Operations | [configuration](https://iqvo.github.io/product-discovery/docs/operations/configuration) (every variable per binary), [runbook](https://iqvo.github.io/product-discovery/docs/operations/runbook) (deploy, probes, migrations, topics and dead letters, rebuild), [observability](https://iqvo.github.io/product-discovery/docs/operations/observability) (metrics, spans, logs, dashboard and alerts), [troubleshooting](https://iqvo.github.io/product-discovery/docs/operations/troubleshooting) (symptom, cause, check, fix), [before the first release](https://iqvo.github.io/product-discovery/docs/operations/before-first-release) |
| Development | [testing](https://iqvo.github.io/product-discovery/docs/development/testing) (the pyramid, `make` targets, CI jobs) |
| Domain-driven design | [use cases](https://iqvo.github.io/product-discovery/docs/ddd/use-cases), [subdomain classification](https://iqvo.github.io/product-discovery/docs/ddd/subdomain-classification), [ubiquitous language](https://iqvo.github.io/product-discovery/docs/ddd/ubiquitous-language), [DDD artifacts](https://iqvo.github.io/product-discovery/docs/ddd/ddd-artifacts) with [core domain chart](https://iqvo.github.io/product-discovery/docs/ddd/core-domain-chart), [bounded context canvas](https://iqvo.github.io/product-discovery/docs/ddd/bounded-context-canvas), [context map](https://iqvo.github.io/product-discovery/docs/ddd/context-map), [aggregate design canvas](https://iqvo.github.io/product-discovery/docs/ddd/aggregate-design-canvas), [domain message flow](https://iqvo.github.io/product-discovery/docs/ddd/domain-message-flow), [EventStorming](https://iqvo.github.io/product-discovery/docs/ddd/eventstorming), [class diagram](https://iqvo.github.io/product-discovery/docs/ddd/class-diagram), [entity relationship](https://iqvo.github.io/product-discovery/docs/ddd/entity-relationship), [sequence diagrams](https://iqvo.github.io/product-discovery/docs/ddd/sequence-diagrams), [domain events](https://iqvo.github.io/product-discovery/docs/ddd/domain-events) |
| API reference | [API overview](https://iqvo.github.io/product-discovery/docs/api-reference) (REST, events, A2A), the REST reference generated from [`apis/openapi.yaml`](apis/openapi.yaml), [event catalogue](https://iqvo.github.io/product-discovery/docs/api-reference/events) (from [`apis/asyncapi.yaml`](apis/asyncapi.yaml)) |
| Ecosystem | [integration](https://iqvo.github.io/product-discovery/docs/ecosystem/integration) (every upstream and downstream edge) |
| Decisions | [ADR index](https://iqvo.github.io/product-discovery/docs/adr) (source: [`docs/adr/`](docs/adr/)); the aggregator's [storefront ADR index](https://iqvo.github.io/warehouse-docs/storefront/adr/) |
| In this repo | [`docs/network-ohs-allowlist.yaml`](docs/network-ohs-allowlist.yaml), [`docs/docs/operations/runbook.md`](docs/docs/operations/runbook.md), [`docs/docs/operations/before-first-release.md`](docs/docs/operations/before-first-release.md) |

Build the site locally with `cd docs && npm ci && npm run build`.

## Architecture

Hexagonal / ports & adapters; dependencies point **inward only**. This context
is a pure read side: no aggregate writes, no outbox, no relay — it consumes
and serves.

```
cmd/api/                    composition root: the REST service and the three Kafka consumers
cmd/rebuild/                one-shot ops job: seeds the projections from the producers'
                            paged snapshot reads (ADR 0003, never on a user request)
cmd/a2a/                    read-only A2A agent over the same read use cases (ADR 0007)
internal/
  domain/                   pure fold logic: productview (fold rules), categoryview,
                            search, shared
  application/
    ports/                  OUT interfaces only (repos, Clock, metrics)
    readmodel/              the projection fold logic's application side
    usecases/               one struct per use case
  adapters/
    inbound/                http (chi), kafka (the three consumers), a2a, requestlog
    outbound/               postgres, kafka (consumer plumbing), telemetry, sources
                            (the producer snapshot clients used by cmd/rebuild:
                            offer-availability, and the storefront clients for
                            pricing GET /item-prices and product-content
                            GET /product-contents + GET /categories),
                            memory (test doubles), repocontract
    kafka/cloudevents/      the CloudEvents 1.0 Kafka reader/writer plumbing
  architecture/             the fitness tests below
  bootretry/                bounded Postgres/migration boot retry
  bdd/                      godog wiring for features/
```

The layering is enforced as an executable test, not a convention:
`internal/architecture/architecture_test.go` (`TestHexagonalArchitecture`,
arch-go) fails the build if the domain ever imports an outer layer or an
adapter reaches sideways into a sibling; the same package adds fitness tests
for the event catalogue vs `apis/asyncapi.yaml`, the A2A adapter's read-only
rule, Kafka writer acks/topics/DLQ shutdown, the outbox relay cap, and
no-Go-files-at-root.

## Domain events

Contract: [`apis/asyncapi.yaml`](apis/asyncapi.yaml) — it records the shape of
the eleven events this service consumes; each producer's own AsyncAPI is
authoritative, unknown fields are ignored (tolerant decoding), and only the
fields this service reads are listed.

**Published** — nothing: `publishes: []` in the model, so there is no outbox,
no relay and no `retailnetwork.product-discovery.events` topic.

**Consumed** — three topics, three stable consumer groups (one per topic, each
named by a mandatory environment variable), with an inbox, per-key monotonic
guards and dead letters:

- from `retailnetwork.offer-availability.events`
  (`com.retailnetwork.commerce.offer-availability.<entity>.<Event>`):
  `CatalogItemCreated`, `CatalogItemDiscontinued`, `ListingCreated`,
  `ListingAvailabilityChanged`, `ListingSuppressed`, `ListingRestored`
- from `retailnetwork.product-content.events`
  (`com.retailnetwork.storefront.product-content.<entity>.<Event>`):
  `ProductContentPublished`, `ProductContentRetired`, `CategoryTreePublished`
- from `retailnetwork.pricing.events`
  (`com.retailnetwork.storefront.pricing.itemprice.<Event>`):
  `ItemPriceChanged`, `ItemPriceWithdrawn`

Every message is a CloudEvents 1.0 event in structured content mode (Kafka
header `content-type: application/cloudevents+json; charset=UTF-8`); the
consumers continue the trace carried in the event's `traceparent` header and
drop replays via the per-key guards.

## Running it

Real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` and the three
`*_CONSUMER_GROUP` variables are mandatory; a boot error names the missing
variable). Migrations run automatically at boot. There is no compose file in
this repo — point the variables at any Postgres/Kafka (the fleet's
`warehouse-infra` kind cluster or the integration tests' testcontainers both
work).

```sh
export DATABASE_URL="postgres://user:***@localhost:5432/product_discovery?sslmode=disable"
export KAFKA_BROKERS="localhost:9092"
export OFFER_AVAILABILITY_CONSUMER_GROUP=product-discovery-offer-availability
export PRODUCT_CONTENT_CONSUMER_GROUP=product-discovery-product-content
export PRICING_CONSUMER_GROUP=product-discovery-pricing
go run ./cmd/api      # search/browse/detail on :8080; the three consumers in the same process
```

`GET /readyz` is 200 only while Postgres answers a ping AND a Kafka broker
answers a metadata request; `/healthz` is liveness only.

`cmd/rebuild` (see below) is run operator-side when the projections need a
re-seed; it never runs on a user request.

### Configuration

`cmd/api`:

| Env var | Default | Meaning |
|---|---|---|
| `HTTP_ADDR` | `:8080` | REST listen address |
| `DATABASE_URL` | **required** | Postgres DSN; unset ⇒ boot error naming the variable |
| `MIGRATIONS_DATABASE_URL` | `DATABASE_URL` | Direct (non-pooled) DSN for the boot migrations |
| `KAFKA_BROKERS` | **required** | Comma-separated brokers (consumers, dead letters, `/readyz`) |
| `OFFER_AVAILABILITY_CONSUMER_GROUP` | **required** | Stable group id, one per consumed topic |
| `PRODUCT_CONTENT_CONSUMER_GROUP` | **required** | |
| `PRICING_CONSUMER_GROUP` | **required** | |
| `PRICE_BAND_EDGES` | `2500,5000,10000,25000` | Ascending minor-unit edges of the price facet |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | Wait after flipping `/readyz` before closing |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `OTEL_SERVICE_NAME` | `product-discovery` | `service.name` of spans and metrics |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | in-cluster Collector | OTLP/gRPC endpoint; export never blocks |
| `SERVICE_VERSION` | `dev` | `service.version` |
| `ENVIRONMENT` | `local` | `deployment.environment.name` |

`cmd/rebuild` — `DATABASE_URL`, `OFFER_AVAILABILITY_URL`, `PRICING_URL` and
`PRODUCT_CONTENT_URL` (the producers' base URLs) are **all required** (the
error names the variable); plus `LOG_LEVEL` and the optional telemetry
variables (`OTEL_SERVICE_NAME` defaults to `product-discovery-rebuild`; the
job exports on exit, and a missing Collector costs telemetry, never the run).
It needs no Kafka: `cmd/api`'s consumers resume from their committed offsets
afterwards and drop every replayed event that is not newer.

`cmd/a2a` reads `DATABASE_URL` and `A2A_AGENT_URL` (both **required**),
`A2A_ADDR` (default `:8093`), `LOG_LEVEL` and the optional telemetry variables
(`OTEL_SERVICE_NAME` defaults to `product-discovery-a2a`). It has no Kafka and
no write.

## Observability

Logs, metrics and traces follow the fleet standard ([ADR
0005](docs/adr/0005-fleet-observability-standard.md)): JSON logs on stdout
carry `trace_id`/`span_id` and a `request_id`; `cmd/api`, `cmd/rebuild` and
`cmd/a2a` export OTLP/gRPC to the Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`,
`OTEL_SERVICE_NAME`, `SERVICE_VERSION`, `ENVIRONMENT`; export never blocks, so
no Collector costs telemetry, nothing else); the consumers continue the trace
carried in the `traceparent` header of each consumed event.

| Instrument | Kind | Source |
|---|---|---|
| `discovery.searches.served` | counter, attributes `outcome`, `reason` | **this service's business metric** |
| HTTP request duration | histogram + one server span and request-log line per call | the HTTP adapter, sharing a trace id |
| `go.*` runtime metrics | various | OTel Go runtime instrumentation |

## API

Contract: [`apis/openapi.yaml`](apis/openapi.yaml). No authentication. Errors
are RFC 9457 problem details (`application/problem+json`). The surface is
read-only — there is no `Idempotency-Key` because nothing writes (no `POST` or `PATCH`).
`GET /products` pages with `limit` (1-500, default 100) and the opaque `cursor`, and answers
`{items, facets, categorySuggestions, nextCursor}`; an undeclared query parameter (the retired `after`) is
`400 unknown-query-parameter`. `GET /categories` is a single versioned tree (`x-pagination-exempt: view`).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/products` | Full-text search (pg_trgm typo fallback), filters, facets, keyset paging |
| `GET` | `/products/{catalogItemId}` | Product detail |
| `GET` | `/categories` | The category tree with purchasable counts |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness (Postgres ping + Kafka metadata) |

## Helm chart

[`charts/product-discovery`](charts/product-discovery) — one always-on
component: the `api` Deployment (REST and the three consumers in one process;
there is no outbox relay). The chart refuses to render without `database.*`,
`kafka.brokers` and the three `config.*ConsumerGroup` values (named in the
failure). `DATABASE_URL` always comes from a Secret.

```sh
helm install product-discovery ./charts/product-discovery \
  --set database.existingSecret=product-discovery-db \
  --set kafka.brokers=kafka:9092 \
  --set config.offerAvailabilityConsumerGroup=product-discovery-offer-availability \
  --set config.productContentConsumerGroup=product-discovery-product-content \
  --set config.pricingConsumerGroup=product-discovery-pricing
```

`cmd/rebuild` is rendered as a ONE-SHOT ops Job (ADR 0006), **default off**:
enable `rebuild.enabled=true` for one `helm upgrade`, let it run to
completion, then disable it again (or run the binary operator-side; see
[`docs/docs/operations/runbook.md`](docs/docs/operations/runbook.md)). The Job runs the
same image with `command: ["/app/rebuild"]`, one run with one retry
(`backoffLimit: 1`), kept for a day for inspection, and reads `DATABASE_URL`
plus the three `rebuild.sources.*Url` values (all required when enabled).

Telemetry is the `otel` values block (`enabled: true` by default, safe where
no Collector is deployed). Chart invariants are asserted by
`charts/product-discovery/tests/` (`render_test.sh`,
`test_service_selectors.py`), run in CI by the `helm-lint` (`ct lint`) and
`chart-tests` jobs.

### Running the A2A agent in Kubernetes

`cmd/a2a` serves an Agent2Agent agent (a2a-go v2, JSON-RPC over HTTP) that
delegates two **read** goals to this context (ADR 0007). A2A carries
delegation, never data: facts between contexts still travel as CloudEvents on
Kafka.

| | |
|---|---|
| Agent Card | `GET /.well-known/agent-card.json` |
| JSON-RPC | `POST /` and `POST /a2a` (method `SendMessage`, header `A2A-Version: 1.0`) |
| Liveness | `GET /healthz` |
| Listen address | `A2A_ADDR` (default `:8093`) |
| Advertised URL | `A2A_AGENT_URL` (required, absolute http(s) URL) |
| Skills | `search_products` (`q`, `categoryId`, `inStock`, `priceMin`, `priceMax`, `sort`, `limit`, `after`), `get_product` (`catalogItemId`) |

A request is a message part
`{"skill":"get_product","arguments":{"catalogItemId":"I-1"}}`; the reply
carries the envelope `{skill, data | problem}` and a one-line text summary.
Rejections come back in-band as problems with REST's statuses (400 malformed,
404 not found, 422 validation failed, 500).

Kubernetes: `a2a.enabled=true` renders `<release>-a2a` (Deployment, Service,
optional HTTPRoute `a2a.route.*` at `/api/a2a/product-discovery`, rewritten to
`/`). It is **off by default**.

## Local development / quality gate

Every CI sensor is a `make` target; `make help` lists them all:
`make check` (fast), `make check-all`, `make integration` (Docker),
`make mutation` (every domain package), `make vuln`.

```sh
make check-fast  # fmt-check, vet, arch-test (+ tests of changed packages)
make check       # fmt-check, vet, build, lint, test
make check-all   # check + coverage (90% gate over domain+application) + arch-test + bdd
```

Git hooks are managed by [lefthook](https://github.com/evilmartians/lefthook)
(`lefthook.yml`): pre-commit runs `fmt-check` + `vet` + `lint`; pre-push runs
`make check`. Activate once per clone:

```sh
brew install lefthook   # or: go install github.com/evilmartians/lefthook@latest
lefthook install
```

## Tests

```sh
go test ./... -race                        # unit suites
make coverage                              # domain+application coverage, CI gate >= 90%
go test -tags=integration ./...            # testcontainers Postgres/Kafka (Docker only)
go test ./internal/bdd/ -run TestFeatures -v   # godog/BDD (features/*.feature) over the real HTTP router
go test ./internal/architecture/... -v     # architecture fitness tests (arch-go)
gremlins unleash ./internal/domain         # mutation, thresholds in .gremlins.yaml
```

CI (`.github/workflows/ci.yml`) runs `lint`, `guide-lint`, `complexity`,
`test` (unit + the 90% coverage gate), `bdd`, `integration`, `mutation-fast`
(the blocking subset), `api-lint` (Spectral over `apis/openapi.yaml`), `vuln`
(govulncheck), `helm-lint` (`ct lint`), `chart-tests`, and `arch-test` on
every push (branches `main`, `develop`) and pull request. The exhaustive
`mutation` job runs only on the weekly schedule (Monday 06:00 UTC) and manual
dispatch, opening a `harness:red` issue on a scheduled failure.
`docker-publish` (GHCR, cosign-signed) and `release` (tag `vX.Y.Z` + chart
publish) run only on pushes to `main`, after every other job is green.

## Architecture Decision Records

[`docs/adr/`](docs/adr/) — [index](docs/adr/index.md); 7 ADRs (0001 bounded
context, 0002 assumptions, 0003 consumed contracts and rebuild, 0004
CloudEvents envelope and type catalogue, 0005 fleet observability standard,
0006 rebuild as default-off Helm job, 0007 A2A inbound adapter).

## License

[MIT](LICENSE).
