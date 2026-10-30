# ADR 0007: A2A inbound adapter

## Status

Accepted (2026-10-30).

## Context

The fleet's agents delegate goals to the bounded context that owns the answer over the Agent2Agent (A2A) protocol.
The three transports keep distinct jobs: Kafka with CloudEvents carries **facts** ([ADR 0004](./0004-cloudevents-envelope-and-type-catalogue.md)),
MCP carries an agent's **tools**, and A2A carries the **delegation of a goal**, never a data sync. This context
(the read side of browsing and search, [ADR 0001](./0001-product-discovery-bounded-context.md)) had only REST:
`GET /products`, `GET /products/{catalogItemId}` and `GET /categories`. It has no `cmd/mcp` (see
[ADR 0005](./0005-fleet-observability-standard.md): "only `cmd/api` and `cmd/rebuild`"), so there is no MCP charter to
derive skills or a write gate from. The context also publishes nothing: it folds three upstream feeds into its
projections and answers reads.

## Decision

Add a **read-only** A2A server as a third deployable, `cmd/a2a`, beside `cmd/api` (REST plus the three consumers)
and `cmd/rebuild` (the ops job, [ADR 0006](./0006-rebuild-as-default-off-helm-job.md)).

**Adapter.** `internal/adapters/inbound/a2a`, on `github.com/a2aproject/a2a-go/v2` (pinned v2.7.0): a message-only
agent (every skill is request/response; no Task lifecycle) answering over JSON-RPC 2.0. It depends on the application
and domain layers only, and nothing depends on it (`internal/architecture/a2a_fitness_test.go`).

**Two curated, goal-shaped skills, both reads.** With no MCP surface to mirror, each is backed by the read use case a
REST GET already exposes; a skill never exposes more than REST does and there is no new query or behaviour:

| Skill | Goal | Use case (REST equivalent) |
|---|---|---|
| `search_products` | find purchasable products: free text with the pg_trgm typo fallback, category, price and in-stock filters, sort, facets, category suggestions, keyset paging | `Queries.SearchProducts` (`GET /products`) |
| `get_product` | read one product page view by `catalogItemId` | `Queries.GetProduct` (`GET /products/{catalogItemId}`) |

`GET /categories` is not a skill: the category tree reaches an agent as the `categoryPath` of a product, the
`categorySuggestions` and the category facet of a search. The set stays at two (goal-shaped, not one per endpoint).

**READ-ONLY, on purpose.** There is no write in this context's REST surface either, so no skill can change state: no
write use case, unit of work, inbox, consumer, outbox or Kafka client is wired into `cmd/a2a`. The arch test enforces
this with an ALLOWLIST of the identifiers the adapter and `cmd/a2a` may use from `usecases`, `ports` and `postgres`
(AST-based, with its own sensor-bites test), not a denylist of write names. The agent contains no outbound A2A client.

**Wire contract.** A request is the first message part, a Data part (or a Text part holding the same JSON):
`{"skill":"<id>","arguments":{...}}`. Decoding is strict: `DisallowUnknownFields` on the request and on the arguments,
and trailing data is refused, so a misspelled filter (`inStok`) cannot be silently dropped. The reply is one agent
Message carrying a Data part (the envelope `{skill, data | problem}`) and a Text part (a one-line summary), bound to the
caller's `contextId`. The payloads are exactly the REST DTOs (`ProductView`, `ProductSearchResult`), duplicated
because the adapter may not import the REST adapter; `cmd/a2a/router_test.go` compares both surfaces field for field
over the same read model, so neither can expose a field the other withholds.

**Problems are answered in-band**, as RFC 7807-shaped objects inside the envelope, never as JSON-RPC errors, with the
status classes and `type` URIs of the REST adapter (`/problems/<slug>`):

| Status | Slug | When |
|---|---|---|
| 400 | `malformed-request` | unknown skill, unreadable request, unknown argument, wrong type of `catalogItemId`, missing `catalogItemId`, a NUL in a string argument |
| 404 | `not-found` | unknown item, a stub the item master has not announced, an identifier over 128 characters or carrying a NUL |
| 422 | `validation-failed` | a bad search value, including a value of the wrong JSON type (REST's answer for a mistyped query parameter) |
| 500 | `internal-error` | anything else; the cause is logged, never echoed |

There is no 409 (no write) and no 503 (REST has none for these reads). The REST adapter's NUL guard is kept: a U+0000
in a Postgres text parameter would be a 500 (SQLSTATE 22021), so it is a 400 (search arguments) or a 404 (an item id).
No shared error text names a deprecated alias, and the search arguments use REST's names, so the alias rule has
nothing to map. Each skill runs in an OTel span `a2a.skill <id>` (attribute `a2a.skill`); an unknown skill name is
answered without a span, so client-chosen text never becomes a span name.

**Unauthenticated**, like every fleet endpoint (the fleet decision that reverted auth; `TestNoAuthMiddlewareReintroduced`
keeps it out). The Agent Card declares no security schemes.

**Composition root.** `cmd/a2a` keeps only what the two reads need from `cmd/api`: the embedded migrations (idempotent,
the same `MIGRATIONS_DATABASE_URL` fallback), the Postgres product and category repositories and the search port, the
price-band edges and the search counter. It runs on real Postgres only. Environment: `DATABASE_URL` (required),
`A2A_AGENT_URL` (required, an absolute http(s) URL, refused at boot otherwise), `A2A_ADDR` (default `:8093`),
`MIGRATIONS_DATABASE_URL`, `PRICE_BAND_EDGES` (must equal the api's, so the facets agree), `LOG_LEVEL` and the OTel
variables. It serves the JSON-RPC endpoint at `/` and `/a2a`, the Agent Card at `/.well-known/agent-card.json` and
`GET /healthz`. There is no `/readyz`: with no Kafka dependency, liveness is the whole probe.

**Chart.** `a2a-deployment.yaml`, `a2a-service.yaml` and `a2a-route.yaml` under an `a2a:` values block that is
**default OFF**; component label `a2a`, port 8093 (8080 is the api), command `/app/a2a`, `A2A_AGENT_URL` defaulting to
`http://<release>-a2a.<namespace>.svc.cluster.local:8093/` and overridable through `a2a.agentUrl`. The optional
HTTPRoute (`a2a.route.*`) matches `/api/a2a/product-discovery` and rewrites the prefix to `/`, so the Agent Card stays
reachable under the same prefix. The Deployment renders no `KAFKA_BROKERS`, consumer group or publisher variable
(asserted). `tests/render_test.sh` scans `cmd/a2a` for every `<VAR> is required` and every variable it reads and
asserts the chart renders them; `tests/test_service_selectors.py` asserts the Services select exactly one Deployment
each. The Dockerfile already builds every `cmd/*` binary; it now also exposes 8093.

## Consequences

- An agent can search and read the storefront catalogue by delegation, with the outcomes a REST caller would see, and
  with no way to change anything: widening this to a write needs its own ADR and a write on the REST surface first.
- Two deployables read one database. `cmd/a2a` runs the same idempotent migrations as the api, so either may boot first.
- Nothing in this ADR moves a fact: the feeds still arrive by Kafka into `cmd/api`, and `cmd/a2a` neither consumes nor
  publishes. A2A is not a replication channel.
- The envelope and the two DTO sets are a contract: changing a field is a change to the REST contract first
  (`apis/openapi.yaml`), mirrored here, and the parity test fails if only one side moves.
- One more Deployment per release when enabled, one replica; `warehouse-infra` flips it on (separate change, after the
  image publishes `/app/a2a`).
