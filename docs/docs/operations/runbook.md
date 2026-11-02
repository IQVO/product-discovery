---
id: runbook
title: Operations runbook
sidebar_label: Runbook
sidebar_position: 2
---

# Operations runbook

`product-discovery` is the read side of browsing and search: the ProductView and CategoryView
projections, full-text search and facets. It consumes offer-availability, product-content and pricing
events and **publishes nothing**, so there is no outbox relay, no relay interval and no producer topic to
operate. Postgres and Kafka are the only runtime; there is no in-memory or permissive mode. REST and A2A
are unauthenticated fleet-wide.

- Chart: `charts/product-discovery` (released as `oci://ghcr.io/iqvo/product-discovery`).
- Image: `ghcr.io/iqvo/product-discovery:<version>` (the chart's default tag is `v<appVersion>`). One image
  carries all three binaries under `/app`; the entrypoint is `./api`, the other workloads name their binary
  with `command`.
- Workloads the chart can render:

| Workload | Binary | Default | Port | Probes |
| --- | --- | --- | --- | --- |
| Deployment `product-discovery` | `cmd/api` (REST + three consumers) | on | Service 80 to container 8080 | startup, liveness `/healthz`, readiness `/readyz` |
| Deployment `product-discovery-a2a` | `cmd/a2a` (read-only agent) | off (`a2a.enabled`) | Service 8093 | all three on `/healthz` |
| Job `product-discovery-rebuild` | `cmd/rebuild` (snapshot backfill) | off (`rebuild.enabled`) | none | none |

Source: `charts/product-discovery/values.yaml`, `templates/deployment.yaml`, `templates/a2a-deployment.yaml`,
`templates/rebuild-job.yaml`.

## Deploy / upgrade

Mandatory values (the chart refuses to render without them and names the missing one):

| Value | Variable | Why |
| --- | --- | --- |
| `database.url` or `database.existingSecret` | `DATABASE_URL` | real Postgres only |
| `kafka.brokers` | `KAFKA_BROKERS` | the three consumers, the dead letters, `/readyz` |
| `config.offerAvailabilityConsumerGroup` | `OFFER_AVAILABILITY_CONSUMER_GROUP` | stable group id |
| `config.productContentConsumerGroup` | `PRODUCT_CONTENT_CONSUMER_GROUP` | stable group id |
| `config.pricingConsumerGroup` | `PRICING_CONSUMER_GROUP` | stable group id |

```sh
helm upgrade --install product-discovery oci://ghcr.io/iqvo/product-discovery --version <version> -n <ns> \
  --set database.existingSecret=product-discovery-db \
  --set kafka.brokers=<broker>:9092 \
  --set config.offerAvailabilityConsumerGroup=product-discovery-offer-availability \
  --set config.productContentConsumerGroup=product-discovery-product-content \
  --set config.pricingConsumerGroup=product-discovery-pricing
```

- Prefer `database.existingSecret` (a Secret you manage, holding `DATABASE_URL`; optionally
  `MIGRATIONS_DATABASE_URL`, a direct non-pooled DSN for the boot migrations). `DATABASE_URL` always reaches
  the pod through a `secretKeyRef`, never through the ConfigMap.
- The consumer group ids must be **stable** and service-prefixed (for example `product-discovery-pricing`),
  never derived from the pod name: a rescheduled pod resumes from its committed offsets. The chart refuses
  `*_CONSUMER_GROUP` and any `*_MODE` variable in `extraEnv`, and any variable it renders itself.
- `terminationGracePeriodSeconds` (default 60) must exceed `SHUTDOWN_DRAIN_DELAY + 50 s` (drain delay, then
  HTTP 10 s, then the three consumers awaited 10 s each, then 10 s pool-close headroom); the chart refuses
  anything shorter because it would SIGKILL a consumer mid-drain and strand an uncommitted offset.
  `config.shutdownDrainDelay` takes whole seconds (`5s`), minutes (`1m`) or `0`.
- `config.httpAddr` must have the same port as `service.targetPort` (and `a2a.httpAddr` as
  `a2a.service.targetPort`); the render fails otherwise.
- OpenTelemetry: `otel.enabled` (default true) renders `OTEL_EXPORTER_OTLP_ENDPOINT`
  (`otel-collector.observability.svc.cluster.local:4317`) and `OTEL_SERVICE_NAME`; `false` renders no `OTEL_*`
  variable at all and the binary falls back to `localhost:4317`. See [Observability](/docs/operations/observability).
- Exposure is opt-in: `ingress.enabled` or `gatewayApi.enabled` route to the api Service only;
  `a2a.route.enabled` publishes the agent under `/api/a2a/product-discovery` (rewritten to `/`). `a2a.agentUrl`
  defaults to the release's in-cluster Service DNS and must be an absolute http(s) URL.
- Upgrade order does not matter between the api and the agent: both run the same idempotent embedded
  migrations. Use the same `config.priceBandEdges` for both so the price facets agree.

## Probes and readiness

`cmd/api`:

| Probe | Path | Settings | Meaning |
| --- | --- | --- | --- |
| startup | `/healthz` | every 2 s, 30 failures (about 60 s) | the migrations and the first Postgres ping run inside a bounded retry (5 attempts, 1 + 2 + 4 + 8 s) before the listener binds |
| liveness | `/healthz` | initial 5 s, every 10 s | always 200 while the process runs; never flipped by shutdown |
| readiness | `/readyz` | initial 3 s, every 5 s | 200 only when the drain gate is open **and** Postgres answers a ping **and** a Kafka broker answers a metadata request that names no topic |

- The dependency verdict is cached for 3 s and one round of checks is bounded by 2 s. A failing dependency
  is logged as `readiness: dependency is not ready` with `dependency` = `postgres` or `kafka`.
- During a drain `/readyz` answers `503 {"status":"not_ready"}`; with a dependency down it answers
  `503 application/problem+json` (`/problems/dependency-unavailable`, `detail` = `not ready: kafka`).
- Readiness does **not** wait for consumer lag, for topics to exist, for the first message, or for a
  populated read model: an empty database is a ready service that answers empty searches.
- The three Kafka readers dial lazily inside their own goroutines, so a broker outage never blocks boot;
  it only keeps `/readyz` at 503 until a broker answers.
- `cmd/a2a` serves `/healthz` only (no `/readyz`, no Kafka); the chart uses it for all three probes.

Source: `internal/adapters/inbound/http/readiness.go`, `cmd/api/main.go`, `cmd/api/workers.go`.

## Migrations

One embedded migration, `0001_product_discovery_schema`, creates `products`, `category_view`,
`listing_items`, `processed_events`, the `pg_trgm` extension and the search indexes
(`internal/adapters/outbound/postgres/migrations/`). `cmd/api`, `cmd/a2a` and `cmd/rebuild` all call
`RunMigrations` at boot (golang-migrate `Up`; "no change" is success).

- Set `MIGRATIONS_DATABASE_URL` to a **direct** (non-pooled, session-mode) DSN when `DATABASE_URL` goes through
  PgBouncer: golang-migrate's advisory lock does not survive transaction pooling. `cmd/rebuild` migrates
  through `DATABASE_URL` only.
- The database role needs `CREATE EXTENSION` rights for `pg_trgm` on first boot (or the extension created
  beforehand by a DBA).
- A failed migration after the 5 boot attempts exits the process with the last error; the pod restarts. A
  dirty migration state is repaired by an operator in `schema_migrations` (outside this repository).
- `RollbackMigrations` exists for the up/down/up integration test only; no binary calls it.

## Kafka: topics, consumer groups and dead letters

| Topic consumed | Consumer (log name) | Group variable | Dead-letter topic |
| --- | --- | --- | --- |
| `retailnetwork.offer-availability.events` | offer-availability | `OFFER_AVAILABILITY_CONSUMER_GROUP` | `retailnetwork.offer-availability.events.dlq` |
| `retailnetwork.product-content.events` | product-content | `PRODUCT_CONTENT_CONSUMER_GROUP` | `retailnetwork.product-content.events.dlq` |
| `retailnetwork.pricing.events` | pricing | `PRICING_CONSUMER_GROUP` | `retailnetwork.pricing.events.dlq` |

The service produces to no business topic. Its only producer is the dead-letter writer.

How a message is handled (`internal/adapters/inbound/kafka`):

- **At-least-once with an atomic effect.** One message at a time: decode the CloudEvents 1.0 envelope, claim
  the event `id` in `processed_events` and apply the fold in the same Postgres transaction, then commit the
  offset. A redelivery after a crash is skipped by the claim.
- **Skipped and committed past** (WARN `skipping a message that is not a valid CloudEvent`): not a CloudEvent.
  Unknown `type` values are ignored silently.
- **Dead-lettered at once** (WARN `dead-lettering an event that can never be applied`): a known type whose
  `data` cannot be decoded or holds an unusable fact (`shared.ErrInvalidFact`: an id the projection cannot
  hold, a negative quantity).
- **Retried, then dead-lettered**: a transient failure (database error) is retried with backoff 200 ms doubling to 5 s,
  up to 5 attempts, then published to `<topic>.dlq` (ERROR `handling failed after the maximum attempts`).
  The dead-letter publish itself is retried until it succeeds, so a message is never dropped by committing
  past it.
- A DLQ record keeps the original key, value and headers and adds `x-dlq-source-topic`,
  `x-dlq-source-partition`, `x-dlq-source-offset`, `x-dlq-error` and `x-dlq-failed-at`. The writer uses hash
  balancing on the key, `RequireAll` acks and auto-creates the topic (it waits up to 40 x 250 ms for the new
  topic's leader).
- **A broker blip** (idle timeout, reset, closed connection) is retried with the same backoff; after 10
  consecutive failures the consumer returns and the **process exits non-zero** so the orchestrator restarts
  it. A CrashLoopBackOff on this service usually means a lost broker or a poison group, not a bad deploy.
- Nothing replays a DLQ automatically. To reprocess, fix the cause and republish the DLQ record's value to
  the source topic with the same key. A dead-lettered event was never applied (its unit of work rolled back,
  so its `id` is not in `processed_events`) and is processed normally when it comes back.

```sh
kubectl -n <ns> logs deploy/product-discovery -f
kubectl -n <ns> port-forward svc/product-discovery 8080:80
curl -s localhost:8080/readyz | jq .
kcat -C -b <broker> -t retailnetwork.pricing.events.dlq -f '%k %h\n%s\n' -e
```

## Outbox relay and sweepers

There are none. The service has no `outbox` table, no relay, and no scheduled sweeper. Two things grow
without bound and have no pruning job in this repository: `processed_events` (one row per applied
CloudEvent id) and the dead-letter topics. Plan retention on both with your DBA and Kafka operator.

## Running the rebuild

`cmd/rebuild` re-seeds ProductView and CategoryView from the producers' paged snapshot reads
(offer-availability `GET /catalog` and the ops-only `GET /listings`, pricing `GET /item-prices`,
product-content `GET /product-contents` and `GET /categories`), in that order, 500 rows per page
([ADR 0003](/docs/adr/0003-consumed-contracts-and-rebuild), [ADR 0006](/docs/adr/0006-rebuild-as-default-off-helm-job)).
It never runs on a user request. Run it after a schema reset, a lost projection, or a guard-state repair.

- It is safe to run with the service live: the consumers resume from their committed offsets afterwards and
  drop every replayed event that is not newer (per-key monotonic guards).
- Re-running is safe. A failing page aborts the run (each source call is tried 4 times on transport errors and
  5xx answers, with a 30 s timeout per call). A row that cannot be folded is skipped and counted
  (WARN `rebuild skipped a row that cannot be folded`).
- It finishes with an INFO line `rebuild finished` carrying `items`, `listings`, `prices`, `contents`,
  `categories`. A failure logs ERROR `rebuild failed` and exits 1.

### Option A: the chart's Job (preferred)

One `helm upgrade` with the Job enabled, then disable it again:

```sh
helm upgrade product-discovery oci://ghcr.io/iqvo/product-discovery --version <version> -n <ns> \
  --reuse-values \
  --set rebuild.enabled=true \
  --set rebuild.sources.offerAvailabilityUrl=http://offer-availability.<ns>.svc.cluster.local \
  --set rebuild.sources.pricingUrl=http://pricing.<ns>.svc.cluster.local \
  --set rebuild.sources.productContentUrl=http://product-content.<ns>.svc.cluster.local

kubectl -n <ns> logs job/product-discovery-rebuild -f   # ends with "rebuild finished" counters

helm upgrade product-discovery oci://ghcr.io/iqvo/product-discovery --version <version> -n <ns> \
  --reuse-values --set rebuild.enabled=false
```

A missing `rebuild.sources.*` URL fails the render naming the value. The Job runs with
`restartPolicy: Never` and `backoffLimit: 1`; the finished Job is kept for
`rebuild.ttlSecondsAfterFinished` (default 24 h).

### Option B: operator-side, no release change

Run the same image anywhere with the four required variables (the error names a missing one):

```sh
kubectl -n <ns> run product-discovery-rebuild-manual --rm -i --restart=Never \
  --image=ghcr.io/iqvo/product-discovery:<version> \
  --env="DATABASE_URL=$DATABASE_URL" \
  --env="OFFER_AVAILABILITY_URL=http://offer-availability.<ns>.svc.cluster.local" \
  --env="PRICING_URL=http://pricing.<ns>.svc.cluster.local" \
  --env="PRODUCT_CONTENT_URL=http://product-content.<ns>.svc.cluster.local" \
  -- /app/rebuild
```

The image's entrypoint is `./api`; always pass `/app/rebuild` as the command. The job needs no Kafka
configuration.

## Routine procedures

| Task | Procedure |
| --- | --- |
| Check health | `GET /healthz` (liveness), `GET /readyz` (names the failing dependency), logs of the three `consumer running` lines at start |
| Is a consumer stuck? | Lag of the group in Kafka tooling; ERROR lines `handling failed; retrying the same message` repeating on one `offset` |
| Handle a dead letter | Read `x-dlq-error`, fix the cause (usually a producer sending an unusable fact), republish the value to the source topic or accept the loss; see [Troubleshooting](/docs/operations/troubleshooting) |
| Backfill / repair the projections | Run the rebuild (above) |
| Change price bands | Set `config.priceBandEdges` and roll **both** the api and the agent; bands are computed at query time, so no rebuild is needed |
| Rotate the database credential | Update the Secret, then `kubectl rollout restart deploy/product-discovery` (and the agent) |
| Roll back a release | `helm rollback`; the schema has a single migration, so there is nothing to undo |
| Pre-release checklist | [Before the first release](/docs/operations/before-first-release) |

## Scaling

`autoscaling.api.enabled` renders an HPA (CPU utilisation 70 %, min 1 / max 4) and the Deployment omits
`replicas`. N>1 is safe for the consumers (stable group ids, inbox dedupe on the CloudEvents `id` in the same
transaction, per-key monotonic guards) but it has not been load-tested. Consumers beyond the partition count
of a topic idle. The agent scales with `a2a.replicaCount`; it holds no state.
