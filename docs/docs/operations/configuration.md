---
id: configuration
title: Configuration
sidebar_label: Configuration
sidebar_position: 1
---

# Configuration

`product-discovery` ships **three binaries** (`cmd/api`, `cmd/a2a`, `cmd/rebuild`). Each is
configured only through environment variables: there is no config file, no flag and no
secret-manager integration in the repository. REST and A2A are unauthenticated fleet-wide, so there is
no token or key to configure.

The service runs on real Postgres and real Kafka only: `cmd/api` has no in-memory mode and no
consumer that can be switched off. A missing mandatory variable is a boot error that names it, raised
before any connection is opened and before a listener binds. Test-only variables do not exist in this
repository (the integration tests start their own containers). Semantics below were taken from reading
the code; nothing here is a captured run.

## `cmd/api` (REST and the three Kafka consumers)

| Variable | Default | Required? | Meaning | Source |
| --- | --- | --- | --- | --- |
| `HTTP_ADDR` | `:8080` | no | Listen address of the REST server (also serves `/healthz` and `/readyz`). | `cmd/api/main.go` (`run`) |
| `DATABASE_URL` | none | **yes** | Postgres DSN. Missing = boot error `DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode`. The embedded migrations run at boot. | `cmd/api/config.go` (`loadRuntime`) |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct (non-pooled) DSN used only for the boot migrations, because golang-migrate's advisory lock does not survive PgBouncer transaction pooling. Empty falls back to `DATABASE_URL`. | `cmd/api/main.go` (`buildDeps`) |
| `KAFKA_BROKERS` | none | **yes** | Comma-separated broker list (entries trimmed, blanks dropped). Missing = boot error `KAFKA_BROKERS is required: this context runs on real Kafka (consumers, dead letters, /readyz)`. In the fleet's kind cluster the single broker is reachable from the host at `localhost:9092`. | `cmd/api/config.go` (`loadRuntime`, `splitList`) |
| `OFFER_AVAILABILITY_CONSUMER_GROUP` | none | **yes** | Stable Kafka consumer group id of the offer-availability consumer (topic `retailnetwork.offer-availability.events`). No default and no literal in code; the deployment chooses it. Missing = boot error `OFFER_AVAILABILITY_CONSUMER_GROUP is required: the consumer always runs on real Kafka`. | `cmd/api/config.go` (`consumerGroupEnvs`), `cmd/api/workers.go` |
| `PRODUCT_CONTENT_CONSUMER_GROUP` | none | **yes** | Group id of the product-content consumer (topic `retailnetwork.product-content.events`). | `cmd/api/config.go`, `cmd/api/workers.go` |
| `PRICING_CONSUMER_GROUP` | none | **yes** | Group id of the pricing consumer (topic `retailnetwork.pricing.events`). | `cmd/api/config.go`, `cmd/api/workers.go` |
| `PRICE_BAND_EDGES` | empty = `2500,5000,10000,25000` | no | Ascending positive integers in minor units; they cut the price facet into bands. A non-ascending, zero, negative or non-numeric list is a boot error (`PRICE_BAND_EDGES "..." must be ascending positive integers (minor units)`). Keep it equal to the A2A agent's value so both facets agree. | `cmd/api/config.go` (`parseBandEdges`) |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | no | After SIGINT/SIGTERM `/readyz` flips to 503 and the process waits this long before closing the listener. `0` disables the wait. Negative or unparsable values log a WARN (`ignoring invalid SHUTDOWN_DRAIN_DELAY`) and use `5s`. Read when the signal arrives, not at boot. | `cmd/api/config.go` (`shutdownDrainDelay`) |
| `LOG_LEVEL` | `info` | no | `debug`, `info`, `warn` (or `warning`), `error`, case-insensitive. Any other value silently falls back to `info`. Logs are JSON on stdout. | `cmd/api/config.go` (`newLogger`) |
| `OTEL_SERVICE_NAME` | `product-discovery` | no | `service.name` of spans and metrics (and the `app` label Loki sees). | `cmd/api/telemetry.go` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint, `host:port` or URL. Export never blocks: an unreachable Collector costs telemetry, never availability. | `cmd/api/telemetry.go`, `internal/adapters/outbound/telemetry/telemetry.go` |
| `SERVICE_VERSION` | `dev` | no | `service.version` resource attribute. | `cmd/api/telemetry.go` |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name` resource attribute. | `internal/adapters/outbound/telemetry/telemetry.go` |

`cmd/api` reports one problem at a time (the first one wins): it checks `DATABASE_URL`, then
`KAFKA_BROKERS`, then the three consumer groups in the order above, then `PRICE_BAND_EDGES`.

## `cmd/a2a` (read-only Agent2Agent agent)

The agent reads Postgres only. It has no Kafka variable, starts no consumer and publishes nothing
([ADR 0007](/docs/adr/0007-a2a-inbound-adapter)).

| Variable | Default | Required? | Meaning | Source |
| --- | --- | --- | --- | --- |
| `A2A_ADDR` | `:8093` | no | Listen address of the agent (JSON-RPC on `/` and `/a2a`, Agent Card, `/healthz`). | `cmd/a2a/config.go` (`defaultAddr`) |
| `A2A_AGENT_URL` | none | **yes** | Absolute `http(s)` URL advertised on the Agent Card, for example `http://product-discovery-a2a.warehouse-systems.svc.cluster.local:8093/`. Missing, relative, or a non-http(s) scheme is a boot error (`A2A_AGENT_URL=... is not an absolute http(s) URL`). | `cmd/a2a/config.go` (`checkAgentURL`) |
| `DATABASE_URL` | none | **yes** | Postgres DSN of the same database `cmd/api` writes. Missing = boot error. | `cmd/a2a/config.go` (`loadBootConfig`) |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct DSN for the boot migrations (the same idempotent migrations `cmd/api` runs). | `cmd/a2a/config.go` |
| `PRICE_BAND_EDGES` | empty = `2500,5000,10000,25000` | no | As in `cmd/api`; keep equal to the api's so facets agree. Same validation. | `cmd/a2a/config.go` (`parseBandEdges`) |
| `LOG_LEVEL` | `info` | no | As in `cmd/api`. | `cmd/a2a/config.go` (`newLogger`) |
| `OTEL_SERVICE_NAME` | `product-discovery-a2a` | no | `service.name` of spans and metrics. | `cmd/a2a/telemetry.go` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint. | `cmd/a2a/telemetry.go` |
| `SERVICE_VERSION` | `dev` | no | `service.version` and the version shown on the Agent Card. | `cmd/a2a/main.go` (`newAgent`) |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name`. | `internal/adapters/outbound/telemetry/telemetry.go` |

All problems are reported together (`errors.Join`), so an operator fixes the environment in one pass.

## `cmd/rebuild` (ops job: snapshot backfill)

The job never runs on a user request. It needs no Kafka configuration
([Runbook](/docs/operations/runbook#running-the-rebuild)).

| Variable | Default | Required? | Meaning | Source |
| --- | --- | --- | --- | --- |
| `DATABASE_URL` | none | **yes** | Postgres DSN; the job runs the embedded migrations against it and folds the snapshots into the projections. Missing = `DATABASE_URL is required`. | `cmd/rebuild/main.go` (`requiredEnv`) |
| `OFFER_AVAILABILITY_URL` | none | **yes** | Base URL of offer-availability (`GET /catalog`, ops-only `GET /listings`). | `cmd/rebuild/main.go` |
| `PRICING_URL` | none | **yes** | Base URL of pricing (`GET /item-prices`). | `cmd/rebuild/main.go` |
| `PRODUCT_CONTENT_URL` | none | **yes** | Base URL of product-content (`GET /product-contents`, `GET /categories`). | `cmd/rebuild/main.go` |
| `LOG_LEVEL` | `info` | no | Parsed with `slog.Level.UnmarshalText`: `debug`, `info`, `warn`, `error` (an unparsable value falls back to `info`). | `cmd/rebuild/main.go` (`newLogger`) |
| `OTEL_SERVICE_NAME` | `product-discovery-rebuild` | no | `service.name`. The job exports once on exit; a missing Collector costs telemetry, never the run. | `cmd/rebuild/main.go` (`startTelemetry`) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint. | `cmd/rebuild/main.go` |
| `SERVICE_VERSION` | `dev` | no | `service.version`. | `cmd/rebuild/main.go` |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name`. | `internal/adapters/outbound/telemetry/telemetry.go` |

The rebuild does not read `PRICE_BAND_EDGES` (bands are computed at query time, not stored), nor
`MIGRATIONS_DATABASE_URL` (it migrates through `DATABASE_URL`).

## Readiness

`GET /readyz` on `cmd/api` is 200 only when the shutdown drain gate is open **and** Postgres answers a
ping **and** a Kafka broker answers a metadata request that names no topic (reachability only). The
verdict is cached for 3 s and a round of checks is bounded by 2 s. `/healthz` is liveness only.
`cmd/a2a` serves `/healthz` but has no `/readyz`. Topics are not part of readiness. Details in
[Runbook](/docs/operations/runbook#probes-and-readiness).

## Compiled-in settings (not configurable)

These are constants in the source; changing them needs a code change.

| Setting | Value | Source |
| --- | --- | --- |
| HTTP `ReadHeaderTimeout` (api, a2a) | 5 s | `cmd/api/main.go`, `cmd/a2a/main.go` |
| HTTP graceful shutdown timeout / per-consumer drain timeout | 10 s / 10 s | `cmd/api/main.go` (`shutdownTimeout`, `workerDrainTimeout`) |
| Telemetry flush on shutdown | 5 s | `cmd/api/telemetry.go` (`telemetryFlushTimeout`) |
| Postgres pool size and statement timeout | 10 connections, `statement_timeout = 5s` | `internal/adapters/outbound/postgres/pool.go` |
| Boot retry (migrations, first ping) | 5 attempts, backoff 1 s, 2 s, 4 s, 8 s (15 s total) | `internal/bootretry/bootretry.go` |
| Consumer retry backoff | 200 ms doubling to a 5 s cap | `internal/adapters/inbound/kafka/kafka.go` (`DefaultRetryInitial`, `DefaultRetryMax`) |
| Handling attempts before a message is dead-lettered | 5 | `internal/adapters/inbound/kafka/deadletter.go` (`domainMaxHandlerAttempts`) |
| Transient broker-connection failures tolerated in a row | 10, then the consumer returns and the process exits non-zero | `internal/adapters/inbound/kafka/kafka.go` (`maxTransientFetchFailures`) |
| Dead-letter topic | `<topic>.dlq` | `internal/adapters/inbound/kafka/deadletter.go` (`DLQSuffix`) |
| Topics consumed | `retailnetwork.offer-availability.events`, `retailnetwork.product-content.events`, `retailnetwork.pricing.events` | `internal/adapters/inbound/kafka/handlers.go` |
| `/readyz` verdict cache / check timeout | 3 s / 2 s | `internal/adapters/inbound/http/readiness.go` |

## What is deliberately absent

- **No Kafka producer variables.** The service publishes nothing, so there is no outbox relay, relay
  interval or max-attempts knob.
- **No consumer mode switches.** All three consumers always run; there is no `*_MODE` variable.
- **No auth variables.** REST and A2A are unauthenticated fleet-wide.
- **No CORS, rate-limit or feature-flag variables.**
- **Chart values.** `charts/product-discovery` maps these variables onto the pod
  (`database.*`, `kafka.brokers`, `config.*`, `rebuild.*`); see [Runbook](/docs/operations/runbook#deploy--upgrade).
