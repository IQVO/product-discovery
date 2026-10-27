# ADR 0005: Adopt the fleet observability standard (logs, metrics, traces)

## Status

Accepted (2026-10-26).

## Context

product-discovery logged JSON with `log/slog` and nothing else: no trace or span id on a log line, no request id, no
request log, no metrics and no traces. The warehouse-systems services (inventory-storage, wes-work-planning) already
share one pipeline and one convention, recorded in their ADRs "standard metrics convention" (inventory-storage ADR 0016,
wes-work-planning ADR 0013), and customer-ordering adopted it in its ADR 0007:

- pods export OTLP/gRPC to the in-cluster Collector (`otel-collector.observability.svc.cluster.local:4317`), which feeds
  Jaeger (traces) and a Prometheus exporter (metrics);
- logs stay on stdout as JSON for Alloy and Loki.

Without the same wiring a search request could not be followed to the consumers that fed its ProductView, a log line could
not be joined to its trace, and no dashboard could show search rate, errors or latency. A service-specific setup was
rejected (every repo should answer the same query the same way), as were OTel auto-instrumentation agents (they cannot
follow a trace through `segmentio/kafka-go` nor name spans by the chi route pattern).

## Decision

Tier 1 (identical in every repo) and Tier 2 (this context's business metric):

1. **Telemetry adapter.** `internal/adapters/outbound/telemetry` holds `Setup` (OTLP/gRPC traces and metrics, W3C
   TraceContext + Baggage propagator, Go runtime metrics; exporters dial lazily, so no Collector means dropped telemetry,
   never a blocked boot or request; returns a shutdown func) and `WithTraceContext` (stamps `trace_id`/`span_id` on a log
   record written with a span-carrying context). Only the composition roots (`cmd/api`, `cmd/rebuild`) and the other
   adapters import it; `internal/domain` and `internal/application` never import OpenTelemetry.
2. **Logger.** `slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, ...)))`, `LOG_LEVEL`, uppercase
   levels. Code on a request or message path uses the `*Context` slog variants; the bare variants have no span.
3. **HTTP middleware order** on the chi router: `RequestID` -> `otelchi.Middleware(serviceName, WithChiRoutes(r))` ->
   `otelchimetric.NewServerRequestDuration` -> `RequestLogger` -> `Recoverer`; the existing `rejectNUL` stays after them.
   `telemetry.Setup` runs before the router is built (otelchimetric binds the global MeterProvider at construction) and
   is flushed last on shutdown. The request line carries `method`, `route` (the chi pattern, never the raw path, which
   holds ids), `status`, `duration_ms`, `bytes` and `request_id`; 5xx is ERROR, 4xx WARN, the rest INFO; `/healthz` and
   `/readyz` are not logged. `RequestLogger` lives in `internal/adapters/inbound/requestlog` as in the rest of the fleet.
4. **Traces over Kafka.** Each inbound consumer extracts `traceparent`/`tracestate` from the message headers and runs
   every handling attempt in a consumer span named `<topic> process` (parent: the publisher's span; a message without the
   headers starts a new trace), so the fold's logs carry the publisher's trace id. The CloudEvents attributes and body
   are untouched (ADR 0004).
5. **Environment contract.** `cmd/api` reads `OTEL_SERVICE_NAME` (default `product-discovery`),
   `OTEL_EXPORTER_OTLP_ENDPOINT` (default `telemetry.DefaultEndpoint`, a bare `host:port` or a URL), `SERVICE_VERSION`,
   `ENVIRONMENT` (read by `Setup`) and `LOG_LEVEL`. `cmd/rebuild`, a batch job, reads the same variables (service name
   default `product-discovery-rebuild`), calls `Setup` and flushes on exit; it gets trace-correlated JSON logs and no router.
6. **Tier 2 business metric.** One counter, `discovery.searches.served`, unit `{search}`, attribute `outcome`
   (`served` | `empty` | `failed`) and, on a failure, `reason` (`validation-failed` | `internal-error`).
   `ports.SearchMetrics` is the application port; `Queries.SearchProducts`, the context's primary use case (the service
   publishes nothing and has no command, so the search is its one business act: `GET /products`), calls it once per call.
   `empty` is a first page (no cursor) with no hit after the trigram retry; a continuation page past the last hit counts
   as `served`. The OTel implementation is in the telemetry adapter and is injected from `cmd/api`; a nil `Deps.Metrics`
   is a no-op. The description tells a human what to conclude: a rising `empty` share means shoppers look for what the
   storefront does not show (check the projection feeds and the catalogue); `failed` with `internal-error` means the search
   itself is failing, `validation-failed` a client sending bad queries.

Not part of this ADR: dashboards and alert rules, which live in warehouse-infra and are generated per deployed context
from the instrument names above.

## Assumptions

- Why `discovery.searches.served` (ADR 0001, `apis/openapi.yaml`): the context is the read side of browsing and search;
  its one thing worth counting is whether a shopper's search is answered with products. The detail and category routes
  are plain reads, covered by `http.server.request.duration`.
- No Helm chart exists yet for the ecommerce repos, so the standard's chart part (`otel.*` values, env on every component,
  `render_test.sh`) is not done here and no chart is created; it follows when the chart does.
- There is no `cmd/mcp`, so only `cmd/api` and `cmd/rebuild` are covered.
- The service publishes nothing, so there is no outbox and no `traceparent` to stamp; only the consumer side is wired.
- `go.mod` pins the OpenTelemetry modules and `golang.org/x/net` to the versions of customer-ordering.

## Consequences

- A search request, its log lines, and the consumer handling that fed the data share trace ids with the upstream
  publishers; `trace_id` in Loki jumps to Jaeger.
- Request rate, errors and duration come from `http.server.request.duration` (otelchimetric) with no hand-rolled counter;
  search health from `discovery.searches.served`.
- Every new log call on a request or message path must use a `*Context` variant, or its line carries no trace id.
- A consumer span is created per handling attempt, so a retried message shows as several spans under the same parent.
