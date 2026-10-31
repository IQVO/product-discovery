# product-discovery operations runbook

product-discovery is the read side of browsing and search: the ProductView and CategoryView projections,
full-text search and facets. It consumes offer-availability, product-content and pricing events and **publishes
nothing** (no outbox relay). Postgres and Kafka are the only runtime; there is no in-memory or permissive mode.

- Chart: `charts/product-discovery` (released as `oci://ghcr.io/iqvo/product-discovery`)
- Image: `ghcr.io/iqvo/product-discovery:<version>` (the chart's default tag is `v<appVersion>`)
- Service: REST on the Service port (default 80 -> 8080), `/healthz` liveness, `/readyz` readiness
  (Postgres ping + a Kafka broker answering metadata, cached 3s)

## Deploy / upgrade

Mandatory values (the chart refuses to render without them, naming the missing one):

| Value | Variable | Why |
|---|---|---|
| `database.url` or `database.existingSecret` | `DATABASE_URL` | real Postgres only |
| `kafka.brokers` | `KAFKA_BROKERS` | the three consumers, the dead letters, `/readyz` |
| `config.offerAvailabilityConsumerGroup` | `OFFER_AVAILABILITY_CONSUMER_GROUP` | stable group id |
| `config.productContentConsumerGroup` | `PRODUCT_CONTENT_CONSUMER_GROUP` | stable group id |
| `config.pricingConsumerGroup` | `PRICING_CONSUMER_GROUP` | stable group id |

Prefer `database.existingSecret` (a Secret you manage, holding `DATABASE_URL`; optionally
`MIGRATIONS_DATABASE_URL`, a direct non-pooled DSN for the boot migrations). The consumer group ids must be
STABLE and service-prefixed (e.g. `product-discovery-pricing`), never derived from the pod name: a rescheduled
pod resumes from its committed offsets.

`terminationGracePeriodSeconds` must exceed `SHUTDOWN_DRAIN_DELAY + 50s` (drain delay, then HTTP 10s, then the
three consumers awaited 10s each, then pool close headroom); the chart refuses anything shorter because it
would SIGKILL a consumer mid-drain and strand an uncommitted offset.

## Inspecting

```sh
kubectl -n <ns> logs deploy/product-discovery -f
kubectl -n <ns> port-forward svc/product-discovery 8080:80
curl -s localhost:8080/readyz | jq .     # postgres + kafka verdicts
```

Dead letters land on `<topic>.dlq` (`retailnetwork.offer-availability.events.dlq`,
`retailnetwork.product-content.events.dlq`, `retailnetwork.pricing.events.dlq`) with the failure reason in the
Kafka headers. The process exits non-zero when a consumer dies on its own, so the orchestrator restarts it —
a CrashLoopBackOff on this service usually means a lost broker or a poison group, not a bad deploy.

## Running the rebuild (snapshot backfill, ADR 0003 S5, ADR 0005)

`cmd/rebuild` re-seeds ProductView and CategoryView from the producers' paged snapshot reads
(offer-availability `GET /catalog` and the ops-only `GET /listings`, pricing `GET /item-prices`,
product-content `GET /product-contents` and `GET /categories`). It never runs on a user request. Run it after
a schema reset, a lost projection, or a guard-state repair. It is safe to run with the service live: the
consumers resume from their committed offsets afterwards and drop every replayed event that is not newer.

### Option A — the chart's Job (preferred)

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

A missing `rebuild.sources.*` URL fails the render naming the value (the binary would exit at boot without it).
The Job runs with `restartPolicy: Never` and `backoffLimit: 1`; the finished Job is kept for
`rebuild.ttlSecondsAfterFinished` (default 24h) for inspection.

### Option B — operator-side, no release change

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

The image's entrypoint is `./api`; always pass `/app/rebuild` as the command for the rebuild. The job needs
no Kafka configuration.

## Scaling

`autoscaling.api.enabled` renders an HPA (CPU utilisation, min 1 / max 4) and the Deployment omits `replicas`.
N>1 is safe for the consumers — stable group ids, inbox dedupe on the CloudEvents `id` in the same transaction,
per-key monotonic guards — but it has not been load-tested.
