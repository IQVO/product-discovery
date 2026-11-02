---
id: troubleshooting
title: Troubleshooting
sidebar_label: Troubleshooting
sidebar_position: 4
---

# Troubleshooting

Symptom, cause, check, fix. Every row comes from a failure mode in the code; the log lines quoted are the
literal `msg` values listed in [Observability](/docs/operations/observability#logs). Product Discovery is a
read model, so the usual question is "why does this product look like this?" and the answer is almost always
which fact has or has not been folded in. Nothing here is a captured run.

Source: `internal/domain/productview`, `internal/adapters/inbound/kafka`, `internal/adapters/inbound/http`,
`internal/adapters/inbound/a2a`, `cmd/*`.

## Does it run? Boot and probes

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| Process exits at once with `service exited with error` naming a variable | A mandatory variable is missing: `DATABASE_URL`, `KAFKA_BROKERS`, one of the three `*_CONSUMER_GROUP` (api); `DATABASE_URL`, `A2A_AGENT_URL` (agent); `DATABASE_URL`, `OFFER_AVAILABILITY_URL`, `PRICING_URL`, `PRODUCT_CONTENT_URL` (rebuild) | The `error` field names the variable; only the first problem is reported by `cmd/api` and `cmd/rebuild`, all of them by `cmd/a2a` | Set it ([Configuration](/docs/operations/configuration)); on the chart, the matching `database.*`, `kafka.brokers`, `config.*` or `rebuild.sources.*` value |
| Exits with `PRICE_BAND_EDGES "..." must be ascending positive integers (minor units)` | Zero, negative, non-numeric or non-ascending edges | The quoted value | Use a strictly ascending list such as `2500,5000,10000,25000` |
| Exits with `A2A_AGENT_URL=... is not an absolute http(s) URL` | Relative URL or a scheme other than http/https | The value | Give the URL clients dial, with scheme and host |
| `retrying` WARN lines for `op` `run migrations` or `ping postgres`, then the pod restarts | Postgres not reachable yet (a sidecar that is not ready, a wrong DSN, a firewall). Five attempts with 1, 2, 4, 8 s pauses, then the last error ends the process | `err` in the WARN lines; the `PodRestarting` alert | Fix the DSN or network; the restart loop recovers once Postgres answers |
| Migration fails with a permission error on `CREATE EXTENSION pg_trgm` | The database role may not create extensions | The migration error in the exit line | Create the extension once as a DBA (`CREATE EXTENSION IF NOT EXISTS pg_trgm;`), or grant the right |
| Migration hangs or fails on a lock when `DATABASE_URL` goes through PgBouncer | golang-migrate's advisory lock does not survive transaction pooling | The DSN host is the pooler | Set `MIGRATIONS_DATABASE_URL` to a direct, session-mode DSN (api and agent); the rebuild job migrates through `DATABASE_URL` only |
| Migration reports a dirty database version | A previous migration run was killed half-way | `schema_migrations` in the service database | An operator repairs the state by hand; there is a single migration (`0001_product_discovery_schema`) |
| `/readyz` answers 503 `application/problem+json`, `detail` `not ready: kafka` (or `postgres`) | The dependency check failed. Kafka means no broker answered a topic-less metadata request within 2 s | WARN `readiness: dependency is not ready` with `dependency` and `error`; the check is cached 3 s | Fix the broker address in `KAFKA_BROKERS` or the network; topics existing or not is irrelevant to readiness |
| `/readyz` answers 503 `{"status":"not_ready"}` | The pod got SIGTERM and is draining | INFO `shutdown: readiness flipped to not-ready; waiting for traffic to drain` | Normal; liveness stays 200 during the drain |
| Process restarts with `<consumer> stopped unexpectedly: exiting so the orchestrator restarts the service` | A consumer returned: the broker was lost for more than 10 consecutive fetch attempts, or a non-transient reader error | ERROR `consumer stopped`, WARN `... broker connection stalled or lost; fetching again after the backoff` (up to 10, backoff 200 ms doubling to 5 s) | Restore the broker; a CrashLoopBackOff on this service usually means a lost broker or a poisoned consumer group, not a bad deploy |
| Pod is SIGKILLed during a rollout and a consumer commits late | `terminationGracePeriodSeconds` shorter than `SHUTDOWN_DRAIN_DELAY` plus the drain budget (HTTP 10 s, each consumer 10 s, pool close) | The chart refuses a shorter value; a hand-built manifest does not | Keep the grace period above `SHUTDOWN_DRAIN_DELAY + 50 s` ([Runbook](/docs/operations/runbook#deploy--upgrade)) |
| Agent pod fails its probes | The agent exposes `/healthz` only; every other path (including `/readyz`) falls through to the JSON-RPC handler, which is not a health answer | The Deployment's probe paths | Use `/healthz` for all three probes (what `charts/product-discovery/templates/a2a-deployment.yaml` does) |

## Why is this product wrong? Folding and visibility

The row for one catalogue item is built by folding facts from three producers. Four things must hold for a
product to be `purchasable`: item `ACTIVE`, content `PUBLISHED`, price `ACTIVE`, and at least one `LIVE`
listing with `availableQty > 0` (`View.Purchasable`, `internal/domain/productview/view.go`). It is listed in
search and browse when the item is `ACTIVE` and the content is `PUBLISHED` (`View.Searchable`). Read the row
with `GET /products/{catalogItemId}` first: `purchasable`, `inStock`, the price fields and `categoryPath`
tell you which fact is missing.

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| `GET /products/{id}` is a 404 `/problems/not-found` for an item that exists in the catalogue | The row is a **stub**: price, content or listing facts arrived, but `CatalogItemCreated` has not (an out-of-order delivery, a lost or dead-lettered event, or a consumer that is behind). A stub is invisible to every read. A very long id (over 128 characters) is also a 404 | Lag of the offer-availability group; the DLQ `retailnetwork.offer-availability.events.dlq`; `SELECT catalog_item_id, title FROM products WHERE catalog_item_id = '<id>'` (a stub has an empty `title`) | Let the consumer catch up, republish the dead letter, or run the rebuild ([Runbook](/docs/operations/runbook#running-the-rebuild)). Never insert rows by hand |
| `GET /products/{id}` returns the product but search never lists it | Search lists only `ACTIVE` items with `PUBLISHED` content. A discontinued item, or one whose content was retired or never published, is readable but not searchable (ADR 0002 A2) | `categoryPath` and `description` are absent when content is not published; `purchasable` is `false` | Publish content upstream, or accept: this is the designed behaviour |
| The product is listed but `purchasable` is `false` | One of the four conditions fails. No price: `effectivePrice` absent. No stock: every listing `SUPPRESSED`, quantity 0, or no listing known | `inStock`, `effectivePrice`, the listing facts on the offer-availability topic | Fix upstream; stock appears with the next `ListingAvailabilityChanged` |
| A price filter or a price sort hides products | Price filters and `sort=price-asc\|price-desc` only list rows whose price is `ACTIVE`; an unpriced row has no position in a price order (ADR 0002 A14) | The same product without the price parameters | Expected; withdraw/restore the price upstream |
| A listing never turns LIVE or stays out of stock after `ListingCreated` | A listing created by an availability fact alone has an unknown state until a state fact arrives; it is still treated as `LIVE` for stock, but a later `ListingSuppressed` is only applied if the listing id is known | `SELECT * FROM listing_items WHERE listing_id = '<id>'` | Ensure the `ListingCreated` or an availability event for that listing reached the service; otherwise rebuild |
| WARN `listing state fact skipped: listing unknown` | A `ListingSuppressed` or `ListingRestored` for a listing id the service never learned (its `ListingCreated` was lost or predates the service). The fact is acknowledged and dropped, not retried (ADR 0002 A1) | `listingId` and `eventId` in the log line | Run the rebuild; it learns the listing from `GET /listings` |
| Stock, price or content does not move after a new event | The per-source guard dropped the event as **stale**: listing `asOf` must be strictly newer, content `revision` strictly higher, price `version` strictly higher. A producer that resets a counter, or an event replayed from an old offset, is ignored silently (the outcome is not logged) | Compare the event's `asOf` / `revision` / `version` with the row (`GET /products/{id}` does not show them; read `products.content_revision`, `products.price_version`, and the `listings` JSON in Postgres) | Have the producer emit a higher value; or fix the data and rebuild |
| Item reappears or never disappears after `CatalogItemDiscontinued` | A discontinue that arrives before the create is remembered, so a late create cannot revive the item; a discontinue never reverts (there is no "reactivate" event) | `products.item_state` is `DISCONTINUED` | Expected |
| An event type is accepted by the producer but has no effect here | Dispatch is on the **full** CloudEvents `type` string. An unknown type is ignored silently and its offset committed | The `type` header against the 11 types in [Events](/docs/api-reference/events) | Fix the producer's type string |
| Category path empty, `purchasableCount` zero everywhere, or `categoryId` filter matches nothing | No `CategoryTreePublished` has been folded yet (the tree is empty at version 0), or the filter id is not in the tree (a `categoryId` not in the tree matches nothing, ADR 0002 A3) | `GET /categories` returns `version: 0` and an empty list before the first tree | Publish the tree upstream or run the rebuild (it skips an unpublished tree) |
| A category rename does not show | The tree is replaced only when its `version` is strictly above the current one | `GET /categories` `version` against the event's | Publish with a higher version |
| Facet `price-band` values differ between REST and the agent | `PRICE_BAND_EDGES` differs between the two Deployments. Bands are computed at query time, so no rebuild is needed | The variable on each pod | Set the same value on both and restart both |

## Search and the API

### REST problem types

Errors are `application/problem+json` with `type` `/problems/<slug>` (`internal/adapters/inbound/http/errors.go`).

| Status | `type` | When |
| --- | --- | --- |
| 400 | `/problems/malformed-request` | A NUL character (U+0000, raw or `%00`) in the query string |
| 404 | `/problems/not-found` | Unknown or stub `catalogItemId`; an id longer than 128 characters |
| 404 | `/problems/resource-not-found` | A NUL character in the URL path (`rejectNUL`); not listed in `apis/openapi.yaml` |
| 400 | `/problems/invalid-limit` | `limit` empty, repeated, not an integer or outside 1..500 (`GET /products`) |
| 400 | `/problems/invalid-cursor` | `cursor` repeated or not a cursor of this search (other sort or matching mode) |
| 400 | `/problems/unknown-query-parameter` | A query parameter the operation does not declare, for example the retired `after` |
| 422 | `/problems/validation-failed` | A filter out of range or of the wrong type; `detail` is `<field>: <reason>` |
| 500 | `/problems/internal-error` | An unmapped error; `detail` is always `an unexpected error occurred` |
| 503 | `/problems/dependency-unavailable` | `GET /readyz` only |

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| 422 on `GET /products` | Validation: `q` over 200 characters; `sort` not one of `relevance`, `price-asc`, `price-desc`, `title`; `priceMin` or `priceMax` negative; `priceMin` above `priceMax`; `inStock` not exactly `true` or `false`; a non-integer price | `detail` names the field, for example `priceMin: must not exceed priceMax` | Correct the parameter |
| 400 `invalid-limit`, `invalid-cursor` or `unknown-query-parameter` on `GET /products` | `limit` outside 1..500, empty or repeated; a `cursor` that is repeated or not of this search; a parameter the operation does not declare (`after`, `offset`, `nextToken`) | `detail` names the parameter | A cursor is bound to the sort and the matching mode: reuse `nextCursor` as `cursor` only with the same `sort`, filters and kind of query (text or browse) |
| 500 `internal-error` on search | A database error. Every Postgres statement has a 5 s `statement_timeout`; a slow query is cancelled and answers 500 | ERROR `request failed` with `error` and `path`; the `discovery.searches.served` counter with `outcome=failed,reason=internal-error` | Check Postgres load and the indexes of migration `0001` (GIN on `doc` and on `title` trigram); the pool is capped at 10 connections per process |
| A typo query (`ketle`) finds nothing | The trigram fallback runs only on the **first page** of a text search whose full-text query matched zero rows, and only for titles whose trigram similarity (whole title or any single word) reaches 0.3 (`search.SimilarityThreshold`) | Try the exact word; `pg_trgm` is installed (`\dx`) | Expected for words far from any title; the fallback cannot cross a cursor (later pages stay in the mode of the first) |
| Search for a product type returns hits but odd ranking | Category names rank at weight A through a query-time boost (ADR 0002 A4); a product-type query also returns up to 5 `categorySuggestions`, shallowest first | The `categorySuggestions` array | Expected |
| Facet counts look larger than the page | Facets are computed over the whole filtered result, not the page (ADR 0002 A7) | `facets` vs `hits` | Expected |
| Empty searches surge | Shoppers look for something the storefront does not show, or the projection is behind | `discovery.searches.served{outcome="empty"}`, consumer lag | Check the feeds first, then the catalogue |

## Messaging: consumers and dead letters

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| Projections stop moving | A consumer is stuck retrying one message, or the group has no members | ERROR `<consumer> handling failed; retrying the same message` repeating with the same `offset`; INFO `consumer running` at start; consumer-group lag | Fix the cause shown in `error` (usually the database). After 5 attempts the message is dead-lettered and the partition moves on |
| Messages land on a `.dlq` topic | Either the event can never be applied (ERROR `... message can never be applied; dead-lettering it without retry`, after WARN `dead-lettering an event that can never be applied`): undecodable `data`, an id the projection cannot hold, a negative quantity or `asOf` of zero, a malformed price (currency not three upper-case letters, version below 1), content revision below 1, or an invalid category tree (duplicate id, unknown parent, a cycle, empty name). Or a transient failure exhausted its 5 attempts (ERROR `... handling failed after the maximum attempts; dead-lettering the message`) | The `x-dlq-error`, `x-dlq-source-topic`, `x-dlq-source-partition`, `x-dlq-source-offset` and `x-dlq-failed-at` headers of the record | A deterministic failure is a producer bug: fix it and republish the corrected value to the source topic with the same key. A dead-lettered event was never applied, so it is processed normally when it returns. Nothing replays a DLQ automatically |
| ERROR `<consumer> dead-letter publish failed; retrying the same message` repeats | The DLQ write fails (the broker refuses to auto-create the topic, no leader yet beyond 40 x 250 ms, an ACL, `RequireAll` acks not reachable). The service never commits past a message it could not dead-letter | `error` in the line | Fix the broker (allow topic auto-creation or create `<topic>.dlq` with the right partitions and replication); the partition stays blocked until then |
| WARN `skipping a message that is not a valid CloudEvent` | A message on a consumed topic that is not CloudEvents 1.0 structured JSON | `topic`, `error` | The producer must publish the full envelope; the message is skipped and committed, not dead-lettered |
| Duplicate delivery did not double-apply | By design: the CloudEvents `id` is claimed in `processed_events` in the same transaction as the fold | `SELECT * FROM processed_events WHERE event_id = '<id>'` | Nothing to fix. Re-using an `id` with new content makes the new event a no-op |
| `processed_events` and the DLQ topics keep growing | There is no pruning job in the repository | Row count, topic size | Plan retention with the DBA and the Kafka operator |
| A new consumer group id re-reads the whole topic | The group ids must be stable. A group derived from the pod name starts from the first offset (the kafka-go default; `readerConfig` sets no `StartOffset`) on every reschedule | `group_id` in the `consumer running` line | Set stable, service-prefixed ids (the chart refuses `*_CONSUMER_GROUP` in `extraEnv`) |

## Rebuild job

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| ERROR `rebuild failed` and exit code 1 | A source page could not be read after 4 attempts (transport error or a 5xx; each call has a 30 s timeout) or answered a non-200 that is not retried, or the database failed. The error text starts with `rebuild catalog`, `rebuild listings`, `rebuild item prices`, `rebuild product contents` or `rebuild categories` | The step name and the `GET <url>` in `error` | Fix the source URL or availability; the job is safe to re-run |
| The `rebuild listings` step fails with a non-200 status | `GET /listings` is the ops-only in-org read of offer-availability (`docs/network-ohs-allowlist.yaml`, kind `ops-exception`), not a storefront route; whatever sits between the job and the producer may not expose it. Only transport errors and 5xx answers are retried, any other non-200 fails at once | The status in `error` | Point `OFFER_AVAILABILITY_URL` at the producer's in-cluster address rather than at a public gateway |
| WARN `rebuild skipped a row that cannot be folded` | A snapshot row holds a fact the projection cannot hold (the same facts the consumers would dead-letter) | `key`, `error`; the final `rebuild finished` counters do not count skipped rows separately | Fix the data upstream and re-run |
| Counters in `rebuild finished` are all zero | The producers returned empty pages, or `PRODUCT_CONTENT_URL` has no published tree (version below 1 is skipped) | The URLs; the producer's own state | Check the sources |
| The Job exists but nothing ran | `rebuild.enabled` is false by default | `helm get values` | Enable it for one upgrade, then disable ([Runbook](/docs/operations/runbook#option-a-the-charts-job-preferred)) |

## A2A agent

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| Clients cannot call the agent although the pod is healthy | The Agent Card advertises `A2A_AGENT_URL`, which must be a URL the client can dial; the in-cluster Service DNS is the chart default | `GET /.well-known/agent-card.json` | Set `a2a.agentUrl` / `A2A_AGENT_URL` to the reachable address |
| Answer is a `problem` with status 400 `malformed-request` | The first message part is not a skill request: no parts, not JSON, no `skill` field, an unknown skill name, an unknown field (decoding is strict: `inStok` is refused, not ignored), a NUL character, or trailing data | `problem.detail` | Send `{"skill":"search_products","arguments":{...}}` as a Data part (or a Text part with that JSON) |
| Answer is a `problem` with status 422 | Same validation as REST (`limit`, `sort`, prices, cursor); a value of the wrong JSON type for a search argument is a 422, for `get_product` a 400 | `problem.detail` | Correct the argument |
| Agent facets differ from the API | `PRICE_BAND_EDGES` differs (see above) | The variable on each Deployment | Align |

An A2A problem is a normal reply, not a JSON-RPC error: the transport answers `200` and the reason is in the
envelope's `problem` field.

## Telemetry

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| No traces or metrics in the backend | The Collector is unreachable (silently dropped), `otel.enabled=false` (no `OTEL_*` variable, the binary then targets `localhost:4317`), or a wrong endpoint | Run with `LOG_LEVEL=debug`: exporter failures are logged as `opentelemetry error`; the INFO line `telemetry configured` shows the endpoint | Fix the endpoint; telemetry never blocks the service |
| Log lines have no `trace_id` | The record was written without a span context (boot, shutdown, plain `slog.Info`) | The line's `msg` | Expected; request and consumer lines carry it |
| The dashboard `Service HTTP RED` panel is empty for the agent | `cmd/a2a` registers no HTTP request histogram | `http_server_request_duration_seconds{service_name="product-discovery-a2a"}` is absent | Expected; use the `a2a.skill` spans and the Kong panels |
