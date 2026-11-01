---
id: observability
title: Observability
sidebar_label: Observability
sidebar_position: 3
---

# Observability

`cmd/api` and `cmd/a2a` follow the fleet observability standard
([ADR 0005](/docs/adr/0005-fleet-observability-standard)): JSON logs on stdout that carry `trace_id` and
`span_id`, an OTLP/gRPC export of traces and metrics, one business counter, and the Go runtime metrics.
`cmd/rebuild` exports the same way once, on exit. This page lists what the code emits; the dashboard and
alert rules listed at the end are the ones that exist in `warehouse-infra` on `develop`. Nothing here is a
captured run.

Source: `internal/adapters/outbound/telemetry/` (`telemetry.go`, `search_metrics.go`, `slogotel.go`),
`cmd/api/telemetry.go`, `cmd/a2a/telemetry.go`, `cmd/rebuild/main.go`.

## Exporter and resource

| Item | Value |
| --- | --- |
| Protocol | OTLP/gRPC for both traces and metrics, to `OTEL_EXPORTER_OTLP_ENDPOINT` (`host:port` or a URL; default `localhost:4317`; a bare `host:port` becomes plaintext `http://`) |
| Blocking | never: the exporters connect lazily, so an unreachable Collector costs telemetry and never availability or boot. Exporter errors are logged at DEBUG as `opentelemetry error` |
| Metric push interval | 15 s (`PeriodicReader`) |
| Trace sampling and batching | SDK defaults with a batch span processor |
| Propagation | W3C `traceparent` / `tracestate` and baggage |
| `service.name` | `OTEL_SERVICE_NAME`; default `product-discovery` (api), `product-discovery-a2a` (agent), `product-discovery-rebuild` (job) |
| `service.version` | `SERVICE_VERSION`, default `dev` |
| `deployment.environment.name` | `ENVIRONMENT`, default `local` |
| Extra resource attributes | `OTEL_RESOURCE_ATTRIBUTES` is honoured by the SDK default resource |
| Flush | on shutdown with a 5 s deadline; a failed flush is logged as WARN `telemetry flush failed on shutdown` and never changes the exit code |

The chart renders `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_SERVICE_NAME` when `otel.enabled` is true (the
default); see [Configuration](/docs/operations/configuration) and the [Runbook](/docs/operations/runbook).

## Metrics

| Instrument | Type | Unit | Attributes | Emitted by | Meaning |
| --- | --- | --- | --- | --- | --- |
| `discovery.searches.served` | Int64 counter | `{search}` | `outcome` = `served`, `empty` or `failed`; `reason` = `validation-failed` or `internal-error` (only when `outcome` is `failed`) | `cmd/api` and `cmd/a2a`, once per `SearchProducts` call (REST `GET /products` and the `search_products` skill) | `served`: a page with hits, or a continuation page; `empty`: a first page that found nothing even after the trigram retry; `failed`: the search answered an error. A rising `empty` share means shoppers look for products the storefront does not show (check the projection feeds); `failed` with `internal-error` means the search itself is failing; `validation-failed` is a client sending bad queries |
| `http.server.request.duration` | Float64 histogram | `s` | `http.method`, `http.scheme`, `http.route` (the chi pattern, for example `/products/{catalogItemId}`) | `cmd/api` only (otelchi metric middleware) | Request duration; bucket bounds 5 ms to 10 s (0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10) |
| Go runtime metrics | various | various | none of its own | `cmd/api`, `cmd/a2a`, `cmd/rebuild` | The OpenTelemetry contrib `runtime` instrumentation (goroutines, heap, GC). The dashboard reads them as `go_goroutine_count` and `go_memory_used_bytes` (with a fallback to the older `runtime_go_*` names) |

The counter is defined in `internal/adapters/outbound/telemetry/search_metrics.go` under the meter name
`github.com/IQVO/product-discovery`; its Prometheus name on the fleet dashboard is
`discovery_searches_served_total`. The use case calls exactly one of `SearchServed`, `SearchEmpty`,
`SearchFailed` per search (`internal/application/usecases/queries_metrics.go`). The other Tier-2 instruments
of the fleet convention do not exist here because the service has no outbox, no relay and no write API: there
is no lag or backlog gauge to export. There is no `/metrics` endpoint and no Prometheus scrape; everything
leaves over OTLP.

`cmd/a2a` has no HTTP request histogram: its router is a plain `net/http` mux
(`cmd/a2a/router.go`) with no otelchi and no access log.

## Traces

| Span | Created by | Kind | Attributes |
| --- | --- | --- | --- |
| `<chi route pattern>` (for example `GET /products`) | otelchi in the REST router (`internal/adapters/inbound/http/server.go`) | server | the otelchi HTTP semantic attributes; the span is named after the route pattern, so there is one name per route and not one per catalogue item |
| `<topic> process` (for example `retailnetwork.pricing.events process`) | the Kafka consumer, once per handling attempt (`internal/adapters/inbound/kafka/tracing.go`) | consumer | `messaging.system=kafka`, `messaging.destination.name=<topic>`, `messaging.operation.name=process`. Parented to the trace in the message's `traceparent` header when the producer sent one, otherwise a new trace. A returned error is recorded on the span and sets its status to error |
| `a2a.skill <skill>` | the A2A adapter (`internal/adapters/inbound/a2a/skills.go`) | internal | `a2a.skill` = `search_products` or `get_product`; the status is set to error when the skill answers a problem. An unknown skill name gets no span (client-chosen text must not become a span name) |

There are no database spans (the Postgres adapter is not instrumented) and no spans for the rebuild's HTTP
reads.

## Logs

JSON via `log/slog` on stdout, one object per line with `time`, `level` and `msg`. `LOG_LEVEL` picks the level
(`debug`, `info`, `warn`/`warning`, `error`; default `info`). The handler is wrapped so that every record
written with a span-carrying context also gets `trace_id` and `span_id` (only the `*Context` slog variants
carry a context; plain `slog.Info` lines at boot and shutdown have none). Probe requests (`/healthz`,
`/readyz`) are not access-logged.

| Where | Level | `msg` | Fields |
| --- | --- | --- | --- |
| Access log (`cmd/api`) | INFO / WARN (4xx) / ERROR (5xx) | `http request` | `method`, `route` (the pattern, never the raw path, which holds ids), `status`, `duration_ms`, `bytes`, `request_id`, `trace_id`, `span_id` |
| REST | ERROR | `request failed` (an unmapped error, answered as 500 `internal-error` with a fixed detail) | `error`, `method`, `path` |
| A2A | ERROR | `a2a skill failed` | `error` |
| Boot | INFO | `telemetry configured` | `service_name`, `otlp_endpoint` |
| Boot | INFO | `postgres adapters configured` (api) / `postgres read side configured` (agent) | none |
| Boot | INFO | `consumer running` (one per consumer) | `consumer` (`offer-availability`, `product-content`, `pricing`), `topic`, `group_id`, `brokers` |
| Boot | INFO | `http server listening` (api) / `a2a agent listening` (agent) | `addr` |
| Boot retry | WARN / INFO | `retrying` / `succeeded after retry` | `op` (`run migrations`, `ping postgres`), `attempt`, `in`, `err` |
| Boot failure | ERROR | `service exited with error` (the process exits 1) | `error` |
| Shutdown | INFO / WARN | `shutdown: readiness flipped to not-ready; waiting for traffic to drain` / `ignoring invalid SHUTDOWN_DRAIN_DELAY` / `worker did not stop before the shutdown drain deadline` | `drain_delay` / `value`, `default` / `worker` |
| Readiness | WARN | `readiness: dependency is not ready` (at most once per 3 s round) | `dependency` (`postgres` or `kafka`), `error` |
| Consumer | WARN | `skipping a message that is not a valid CloudEvent` | `consumer`, `topic`, `error` |
| Consumer | WARN | `dead-lettering an event that can never be applied` | `consumer`, `type`, `event_id`, `error` |
| Consumer | WARN | `listing state fact skipped: listing unknown` (a `ListingSuppressed` or `ListingRestored` for a listing the service never saw, ADR 0002 A1) | `listingId`, `eventId` |
| Consumer | WARN | `<consumer> broker connection stalled or lost; fetching again after the backoff` | `error`, `attempt`, `backoff` |
| Consumer | ERROR | `<consumer> handling failed; retrying the same message` | `error`, `partition`, `offset`, `attempt`, `max_attempts`, `retry_in` |
| Consumer | ERROR | `<consumer> handling failed after the maximum attempts; dead-lettering the message` | `error`, `partition`, `offset`, `attempts` |
| Consumer | ERROR | `<consumer> message can never be applied; dead-lettering it without retry` | `error`, `partition`, `offset` |
| Consumer | ERROR | `<consumer> offset commit failed; retrying the same message`, `<consumer> dead-letter publish failed; retrying the same message` | `error`, `partition`, `offset`, `attempt`, `retry_in` |
| Consumer | ERROR | `consumer stopped` (a consumer returned on its own; the process then exits non-zero) | `consumer`, `error` |
| Rebuild | INFO / ERROR | `rebuild finished` / `rebuild failed` | `items`, `listings`, `prices`, `contents`, `categories` / `error` |
| Rebuild | WARN | `rebuild skipped a row that cannot be folded` | `key`, `error` |
| Rebuild | WARN | `telemetry disabled` (setup failed; the run goes on) | `error` |

`<consumer>` is the consumer name: `offer-availability`, `product-content` or `pricing`.

REST errors are `application/problem+json` documents with `type` `/problems/<slug>`; the slug and status
table is in [Troubleshooting](/docs/operations/troubleshooting#rest-problem-types). The detail of an unmapped
error is the fixed text `an unexpected error occurred`: the real error is only in the `request failed` line.

## Probes

`GET /healthz` (liveness, always 200 while the process runs) and `GET /readyz` (200 only while Postgres
answers and a Kafka broker answers a topic-less metadata request) are described in the
[Runbook](/docs/operations/runbook#probes-and-readiness). The cause of a failed dependency is only in the
WARN line above; the response body names the dependency.

## Dashboard and alerts that exist

Only what `warehouse-infra` has on `develop` is listed.

| Artifact | File in `warehouse-infra` | What it shows |
| --- | --- | --- |
| Grafana dashboard `warehouse-product-discovery` ("Warehouse - Product Discovery") | `terraform/dashboards/contexts/product-discovery.json` | Kong request rate, 5xx ratio and p95 latency (request vs upstream) of the context's routes; service HTTP RED from `http_server_request_duration_seconds` filtered on `service_name=~"product-discovery.*"` (rate and p95 by route/process); the business panel `discovery_searches_served_total` by `outcome`; Go goroutines and memory; Loki panels on `{app="product-discovery"}` (live stream, volume by level, errors and warnings only) |
| Alert `WarehouseKong5xxRatioHigh` (warning) | `terraform/alerting/warehouse-alert-rules.json`, group `warehouse-product-discovery` | more than 5 % of a Kong route's requests answer 5xx over 5 minutes, for 5 minutes |
| Alert `WarehousePodRestarting` (warning) | same group | a `product-discovery-*` container restarted twice or more in 15 minutes (boot-retry exhaustion, a failed migration, a consumer that stopped on purpose, an OOM kill) |
| Alerts `WarehouseApiAvailabilityBurnFast` / `WarehouseApiLatencyBurnFast` (critical) and `...BurnSlow` (warning) | group `warehouse-product-discovery-slo` | multi-window burn rate of the 99 % availability and 95 %-under-500 ms latency SLOs on the Kong routes |

The alerts are Kong-edge and pod-restart alerts only. No alert reads `discovery.searches.served`, consumer
lag or the dead-letter topics: those are not covered by shipped rules.

## Suggested checks that are not shipped

These are built from data that exists; they are suggestions, not rules in the repository.

| Signal | How to read it | Suggested alert |
| --- | --- | --- |
| Projection feed stalled | consumer-group lag of the three groups in Kafka tooling; repeated `handling failed; retrying the same message` for one `offset` | lag growing for several minutes |
| Dead letters | depth of `retailnetwork.offer-availability.events.dlq`, `retailnetwork.product-content.events.dlq`, `retailnetwork.pricing.events.dlq`; WARN `dead-lettering an event ...`, ERROR `... dead-lettering the message` | any message |
| Empty searches | `sum(rate(discovery_searches_served_total{outcome="empty"}[15m])) / sum(rate(discovery_searches_served_total[15m]))` | a sustained jump |
| Failing searches | `discovery_searches_served_total{outcome="failed",reason="internal-error"}` | any sustained rate |
| Not ready | `GET /readyz` 503 `dependency-unavailable`; WARN `readiness: dependency is not ready` | not ready for more than a minute |
| Table growth | row count of `processed_events` (one row per applied CloudEvent id, never pruned) | capacity planning, see the [Runbook](/docs/operations/runbook#outbox-relay-and-sweepers) |
