---
id: testing
title: Testing
sidebar_label: Testing
sidebar_position: 1
---

# Testing

The test pyramid as it is on this branch: a pure domain under a mutation gate, use cases and adapters with
unit and `httptest` tests, a godog suite over the real use cases, testcontainers integration tests against
real Postgres and Kafka, architecture fitness tests, and spec linting. Counts come from reading the test
files (`func Test...` per package); many tests are table-driven, so the number of cases is larger. Nothing on
this page is a captured run.

| Layer | Count / threshold | Command |
| --- | --- | --- |
| Unit, domain | 56 top-level test functions in 4 packages (`productview` 24, `search` 20, `categoryview` 10, `shared` 2) | `make test` |
| Unit, application | 23 in `internal/application/usecases` (6 files, including concurrency and metrics) | `make test` |
| Unit and httptest, adapters and binaries | `inbound/http` 19, `inbound/kafka` 32, `inbound/a2a` 20, `inbound/requestlog` 4, `kafka/cloudevents` 6, `outbound/kafka` 4, `outbound/memory` 1 (the repository contract), `outbound/sources` 7, `outbound/telemetry` 11, `bootretry` 6, `cmd/api` 16, `cmd/a2a` 14, `cmd/rebuild` 6 | `make test` |
| Coverage gate | 90 % over `./internal/domain/...,./internal/application/...` (`COVERAGE_THRESHOLD`) | `make coverage` |
| BDD (godog / Gherkin) | 3 feature files, **18 scenarios** (`search` 8, `folding` 6, `categories` 4), 60 step lines; strict mode | `make bdd` |
| Integration (testcontainers) | 7 tests behind `//go:build integration` in 8 files: `cmd/api` 3, `cmd/a2a` 1, `cmd/rebuild` 1, `outbound/postgres` 2 (plus one `TestMain` in each package that boots the shared container) | `make integration` |
| Contract | The repository contract suite (`internal/adapters/outbound/repocontract`) runs against the in-memory store and against real Postgres; Spectral lints both specs; no consumer-driven or schemathesis contract tests | `make test`, `make integration`, CI `api-lint` |
| Architecture fitness | 33 top-level test functions in `internal/architecture` (14 files); two skip here on purpose, see below | `make arch-test` |
| Mutation (gremlins) | efficacy 99, mutant coverage 99 (`.gremlins.yaml`); the file records a 100 % / 100 % measurement over every package of `internal/domain` | `make mutation`, `make mutation-full` |
| Evals | none: `EVALS.md` at the repository root is the harness template's note about agent evaluation on another service; it measures nothing in this repository | none |

## Make targets

Every target mirrors a CI job; the `Makefile` is the only definition of the coverage scope and the mutation
package list.

| Target | Runs | Notes |
| --- | --- | --- |
| `make help` | prints the target list | default target |
| `make build` | `go build ./...` | compiles the three binaries |
| `make vet` | `go vet ./...` | |
| `make fmt` / `make fmt-check` | `gofmt -w .` / fail when `gofmt -l .` is non-empty | |
| `make lint` | `golangci-lint run --allow-parallel-runners ./...` | pinned `v2.14.0`, config `.golangci.yml` |
| `make test` | `go test ./... -race` | unit, httptest and the godog suite (it is an ordinary `Test` function); no database needed |
| `make coverage` | `go test ./... -race -coverprofile=coverage.out -coverpkg=./internal/domain/...,./internal/application/...`, then the 90 % gate | the CI `test` job runs exactly this; a workflow that defined its own `-coverpkg` would fail the repository lint |
| `make arch-test` | `go test ./internal/architecture/... -v` | |
| `make bdd` | `go test ./... -run TestFeatures -v` | the godog suite, `internal/bdd/features_test.go` |
| `make integration` | `go test -tags=integration ./... -race -count=1` | needs only Docker; every test boots its own Postgres and Kafka; not part of `check` / `check-all` |
| `make mutation` | `gremlins unleash` on each directory of `./internal/domain/*/` (`productview`, `categoryview`, `search`, `shared`) | the fast, blocking gate; a new domain package is gated as soon as it exists |
| `make mutation-full` | `gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30` | the exhaustive scheduled run |
| `make vuln` | `govulncheck ./...` | |
| `make check` | `fmt-check vet build lint test` | what the pre-push hook runs (`lefthook.yml`) |
| `make check-all` | `check coverage arch-test bdd` | run before pushing |
| `make check-fast` | `fmt-check vet arch-test` plus `go test` of the packages changed against `HEAD` | the agent Stop hook |
| `make guide-lint` / `make harness-test` | `scripts/harness/guide_lint.py` / `scripts/harness/test_hook.py` | agent-guide checks, not product tests |

Run one package or one test:

```bash
go test ./internal/domain/productview -run TestPurchasable -v
go test ./internal/application/usecases -v
```

## Unit tests

- **Domain** (`productview`, `search`, `categoryview`, `shared`): pure, standard library only, no fixtures.
  They pin the fold rules and their per-source guards (listing `asOf`, content `revision`, price `version`),
  the stub-row behaviour, the `PurchasableRule`, query validation and the cursor, the price bands, the facet
  tallies, the trigram similarity and category-name matching, and the category tree checks (duplicate id,
  unknown parent, cycle, version guard).
- **Use cases** (`internal/application/usecases`): the consumer folds, the three queries and the rebuild run
  against the in-memory adapters, which are test doubles only: the shipped binaries never import them
  (`runtime_no_memory_fitness_test.go` enforces it). `concurrency_test.go` covers the retry on a
  serialization failure; `queries_metrics_test.go` the `served` / `empty` / `failed` counter.
- **HTTP adapter**: `httptest` over the real router (`products_test.go`, `search_test.go`,
  `readiness_test.go`, `telemetry_test.go`).
- **A2A adapter**: the Agent Card, the skill dispatch table, strict argument decoding and the parity of the
  skill answers with REST (`skills_test.go`, `card_test.go`, `cmd/a2a/router_test.go`).
- **Kafka consumers**: a fake `Reader` drives the run loop (`consume_loop_test.go`: retry, backoff sequence,
  dead letter after 5 attempts, no commit before success), plus `deadletter_test.go`, `dispatch_test.go`,
  `pipeline_test.go`, `tracing_test.go` and `topic_not_ready_test.go`.
- **Rebuild sources**: `sources_test.go` against `httptest` servers (paging, retries on 5xx, status
  handling).
- **Binaries**: environment parsing and boot errors (`cmd/api/main_test.go`, `cmd/a2a/bootconfig_test.go`,
  `cmd/rebuild/main_test.go`), shutdown behaviour, telemetry wiring.

## BDD (godog)

`features/*.feature` holds three files: `search.feature` (8 scenarios: full-text, typo fallback, category
suggestions, AND filters, subtree filter, facets over the whole result, cursor paging, validation
failures), `folding.feature` (6: eleven kinds of fact make one purchasable product, facts before the item
master stay invisible, a discontinued item, a suppressed listing, the per-listing guard, a redelivered
event) and `categories.feature` (4: counts roll up, a rename shows at once, an older tree version is
ignored, an empty list before the first tree). The runner is `internal/bdd/features_test.go` (`TestFeatures`,
godog `Paths: ../../features`, `Strict: true`). It wires the **real use cases over the in-memory store** and
reads through the real HTTP router, so the scenarios are fast and need no Docker.

## Integration (testcontainers)

Every file carries `//go:build integration` and boots its own containers (`internal/testing/pgtest`,
`internal/testing/kafkatest`); there is no `DATABASE_URL` or `KAFKA_BROKERS` gate and no skip.

| Test | Package | What it proves |
| --- | --- | --- |
| `TestRealWiringFoldsEventsFromThreeTopicsIntoOneSearchableProduct` | `cmd/api` | the real entry point on real Postgres and Kafka: events from the three topics fold into one searchable, purchasable product read back over REST |
| `TestRealWiringPoisonMessageGoesToTheDeadLetterTopic` | `cmd/api` | a message that can never be applied lands on `<topic>.dlq` and the consumer moves on |
| `TestRealWiringReadyzTurns503WhenKafkaStops` | `cmd/api` | `/readyz` follows the Kafka broker |
| `TestRealWiringAgentReadsPostgresOverTheWire` | `cmd/a2a` | the agent boots on real Postgres and answers both skills |
| `TestRebuildSeedsARealDatabaseAndIsRepeatable` | `cmd/rebuild` | the rebuild fills the projections from stub snapshot servers, and a second run is harmless |
| `TestPostgresHonoursTheRepositoryContract` | `outbound/postgres` | the shared repository contract (`repocontract.Run`) passes on Postgres exactly as on the in-memory store |
| `TestMigrationsGoUpDownUp` | `outbound/postgres` | the migration applies, reverts and applies again |

`make integration` and the CI `integration` job run the same command. Fitness tests fail the build if an
integration test gates on environment variables or hard-codes a broker address
(`TestPostgresIntegrationTestsUseTestcontainers`, `TestKafkaIntegrationTestsUseTestcontainers`).

## Mutation testing

`.gremlins.yaml` sets `workers: 1`, `timeout-coefficient: 30` and the thresholds `efficacy: 99` and
`mutant-coverage: 99`. gremlins fails when the measured value is less than or equal to the threshold, so a
threshold sits strictly below what the code achieves; the file records a 2026-10-25 measurement of 100 %
efficacy and 100 % mutator coverage over `productview`, `categoryview`, `search` and `shared`. Only the
domain is mutated, not use cases or adapters.

CI runs the fast loop on every push and pull request (`mutation-fast`, blocking) and the exhaustive
`./internal/domain` run on the weekly schedule and on manual dispatch (`mutation`); a failing scheduled run
opens or refreshes a `harness:red` issue (`scripts/harness/red_issue.py`).

## Architecture fitness tests

`internal/architecture` holds the dependency-direction rules (`TestHexagonalArchitecture`: domain depends on
nothing internal, application only on domain, inbound and outbound adapters never on each other, only `cmd`
wires every layer, `ports` holds only interfaces) and source-scanning sensors, among them:

| Test | Guards |
| --- | --- |
| `TestA2AAdapterDependencyRule`, `TestA2ASourcesStayReadOnly` | the A2A adapter depends inward only and never writes |
| `TestNoAuthMiddlewareReintroduced` | no auth middleware (fleet decision) |
| `TestCloudEventsOnly`, `TestNoEventEnvelopeToggleOrFlatEnvelope`, `TestEventCatalogueMatchesContract` | CloudEvents 1.0 only; the consumed type catalogue matches `apis/asyncapi.yaml` |
| `TestKafkaConsumerGroupNeverHardcodedInline`, `TestReplayConsumersSetCommitInterval`, `TestKafkaWritersSetRequiredAcks`, `TestKafkaWriterTopicNotSetTwice`, `TestDeadLetterCallsGuardCancellation` | consumer group from the environment, explicit commits, `RequireAll` acks, no double topic, cancellation-safe dead letters |
| `TestNoInOrgNetworkRoute` | outbound HTTP calls stay within `docs/network-ohs-allowlist.yaml` |
| `TestRuntimeBinaryHasNoInMemoryAdapters`, `TestNoGoFilesAtRepoRoot` | binaries never import the memory adapters; no Go file at the module root |
| `TestPostgresIntegrationTestsUseTestcontainers`, `TestKafkaIntegrationTestsUseTestcontainers` | integration tests start their own containers |

Each sensor that scans source also has a test that proves it can fail (`...SensorBites`,
`...SensorFailsOnBadFixtures`). Two tests skip on purpose because this service has neither: the MCP
optional-input sensor (`mcp_optional_inputs_test.go`, no `internal/adapters/inbound/mcp`) and the outbox
relay cap (`outbox_relay_cap_fitness_test.go`, no relay). `TestMCPAdapterDependencyRule` passes trivially
for the same reason.

```bash
make arch-test
```

## CI jobs

`.github/workflows/ci.yml` runs on push and pull request to `main` and `develop`, weekly on Monday 06:00 UTC
(mutation only) and on manual dispatch.

| Job | Runs | When |
| --- | --- | --- |
| `lint` | `golangci-lint` v2.14.0, `--timeout=5m` | always |
| `guide-lint` | `scripts/harness/guide_lint.py` and `test_hook.py`; advisory (`continue-on-error`) | always |
| `complexity` | golangci-lint with only `gocyclo,gocognit,cyclop,funlen,nestif` (gate 15) plus an informational gocyclo report | always |
| `file-length` | the `revive` file-length limit (at most 300 lines of code per production Go file, settings in `.golangci.yml`): blocking for the Go files a pull request into `develop` adds or touches; an informational report of the whole backlog otherwise | always |
| `test` | `go build`, `go vet`, then `make coverage` (90 % gate); uploads `coverage.out` | always |
| `bdd` | `go test ./... -run TestFeatures -v` | always |
| `integration` | `go test -tags=integration ./... -race -count=1` | always |
| `mutation-fast` | gremlins v0.6.0 on every `./internal/domain/*/` package; blocking | always |
| `mutation` | `gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30`, `harness:red` issue handling | schedule and dispatch only |
| `api-lint` | Spectral on `apis/openapi.yaml` (`.spectral.yaml`) and `apis/asyncapi.yaml` (`.spectral.asyncapi.yaml`), `--fail-severity=warn` | always |
| `vuln` | `govulncheck ./...` | always |
| `helm-lint` | `ct lint` and `helm lint` with `ci/default-values.yaml` and `ci/all-components-values.yaml` | pull requests only |
| `chart-tests` | every `charts/product-discovery/tests/*.sh` and `test_*.py` (`render_test.sh`, `test_service_selectors.py`); a test that prints `SKIP` fails the job | always |
| `arch-test` | `go test ./internal/architecture/... -v` | always |
| `docker-publish` | build and push `ghcr.io/iqvo/product-discovery`, cosign signature, SBOM; needs the jobs above | push to `main` only |
| `release` | tags the next patch version, retags the image, packages and pushes the Helm chart, creates the GitHub release | push to `main` only, after `docker-publish` |

Other workflows: `docs.yml` builds this site on pull requests (and deploys to GitHub Pages only on push to
`main`), `codeql.yml` runs CodeQL, `ai-review.yml` is an advisory review on pull requests. Jobs that show
*skipping* on a pull request into `develop` (`mutation`, `docker-publish`, `release`) are gated by their
`if:` conditions; that is expected.

`ci.yml` has no job that regenerates the REST reference and fails on a diff, so regenerate it yourself after
any change to `apis/openapi.yaml` (see below).

## Reproducing the docs build

```bash
cd docs
npm ci
npx tsc --noEmit -p .
npm run build
```

The build fails on a broken link (`onBrokenLinks` is `throw`). After a change to `apis/openapi.yaml` run
`npm run clean-api-docs` and `npm run gen-api-docs` to refresh `docs/docs/api-reference/rest/`. Mermaid
diagrams are checked with `scripts/validate-mermaid.cjs` of the warehouse-docs repository, because
`docusaurus build` alone never catches a broken diagram.
