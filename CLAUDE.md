# CLAUDE.md — product-discovery

`product-discovery` is the **Product Discovery** bounded context (Supporting
subdomain, `storefront` tier) of the retail-network storefront domain (GitHub
org `IQVO`, Go module `github.com/IQVO/product-discovery`). It is the read side
of browsing and search: one `ProductView` per catalogue item (title, flags,
content, category path, list and effective price, inStock, purchasable) and one
`CategoryView` (tree plus purchasable counts), folded from the events of
offer-availability, product-content and pricing, searched with PostgreSQL
full-text search and pg_trgm behind a port. It owns NO aggregate (ADR 0002) and
publishes nothing. Binaries: `cmd/api` (read-only REST plus the three
consumers in one process), `cmd/rebuild` (operator-run snapshot re-seed,
shipped as an optional default-off Helm Job, ADR 0006) and `cmd/a2a`
(read-only Agent2Agent agent, ADR 0007).

## Hard rules (each one is fitness-tested or decided in an ADR)

1. **Hexagonal dependency rule.** `internal/domain` imports nothing outside the
   domain; `internal/application` only domain and application; inbound and
   outbound adapters never import each other; only `cmd/` wires layers
   (`TestHexagonalArchitecture`, `make arch-test`).
2. **CloudEvents 1.0 is MANDATORY** on every consumed message (structured mode, no flat envelope, no envelope toggle:
   `TestCloudEventsOnly`, `TestNoEventEnvelopeToggleOrFlatEnvelope`). Decode
   only through `internal/adapters/kafka/cloudevents/`; dispatch on the FULL
   `type`; ignore unknown types. Catalogue: ADR 0004, `apis/asyncapi.yaml`
   (`TestEventCatalogueMatchesContract`).
3. **Publishes nothing**: no outbox, no relay, no
   `retailnetwork.product-discovery.events` topic; the only writes to Kafka are
   dead letters to `<topic>.dlq`. Adding a published event needs an ADR.
4. **Folds are guarded per key** (ADR 0003): listing `asOf`, content
   `revision`, price `version`, tree `version`. A fact the guard refuses is
   acknowledged as `Stale` without effect; a withdrawal or retire at a newer
   version is still recorded so an older replay cannot revive the row. A
   well-formed fact never errors; a malformed one is `shared.ErrInvalidFact`
   and is dead-lettered. A cancelled context never dead-letters
   (`TestDeadLetterCallsGuardCancellation`).
5. **Inbox dedupe and stable groups**: every consumer claims the inbox row
   (CloudEvents `id`) and folds in one unit of work; each group id comes from
   its mandatory `*_CONSUMER_GROUP` variable, never a literal
   (`TestKafkaConsumerGroupNeverHardcodedInline`); replay consumers set a
   commit interval (`TestReplayConsumersSetCommitInterval`).
6. **Storefront boundary**: the only outbound HTTP client is
   `internal/adapters/outbound/sources` (the rebuild's snapshot reads), and it
   may call only the Open Host Service routes listed in
   `docs/network-ohs-allowlist.yaml`, never an in-org or restricted route
   (`TestNoInOrgNetworkRoute`). The A2A sources stay read-only
   (`TestA2ASourcesStayReadOnly`).
7. **The REST surface is read-only**: `GET /products`, `GET /products/{catalogItemId}`,
   `GET /categories`; no write route, so no `Idempotency-Key`. A stub row (no
   title yet) is invisible to every read (404). Errors are RFC 9457
   `application/problem+json`. REST guideline: ADR 0008.
8. **No auth layer on REST or A2A** (fleet-wide revert 2026-09-11): never add
   bearer/JWT/API-key middleware (`TestNoAuthMiddlewareReintroduced`).
9. **Real infrastructure only**: `cmd/*` never imports the in-memory adapters
   (`TestRuntimeBinaryHasNoInMemoryAdapters`); Kafka and Postgres integration
   tests use testcontainers. No Go file at the repo root
   (`TestNoGoFilesAtRepoRoot`).
10. **An ADR comes first** for a new consumed contract, a new projection field
    family, a search-engine swap or any auth. ADRs live in `docs/adr/`.

## Commands

```bash
make check-fast   # fmt-check + vet + arch-test + tests of changed packages: run before saying "done"
make check        # fmt-check vet build lint test
make check-all    # check + coverage (90% gate on domain+application) + arch-test + bdd
make bdd          # godog scenarios in features/
make integration  # real Postgres/Kafka via testcontainers (needs Docker)
make mutation     # gremlins on each internal/domain package (CI job mutation-fast, blocking)
make guide-lint   # these guides: skills load, references resolve, context budget
```

Run locally: `DATABASE_URL=... KAFKA_BROKERS=... OFFER_AVAILABILITY_CONSUMER_GROUP=...
PRODUCT_CONTENT_CONSUMER_GROUP=... PRICING_CONSUMER_GROUP=... go run ./cmd/api`
(REST `:8080`, migrations at boot, a missing variable is a boot error).
`cmd/rebuild` needs `DATABASE_URL` plus `OFFER_AVAILABILITY_URL`, `PRICING_URL`
and `PRODUCT_CONTENT_URL` and no Kafka; `cmd/a2a` needs `DATABASE_URL` and
`A2A_AGENT_URL` (`:8093`).

## Where things are written down (read before editing the matching area)

- `.claude/rules/domain-model.md` (always loaded): ubiquitous language,
  projections, guards, use cases. `.claude/rules/rest-api.md`: the routes and
  error slugs. `.claude/rules/integration-events.md`: the eleven consumed
  types, their topics and guards.
- `.claude/rules/fleet/*.md`: fleet-wide rules copied from the harness
  template (CloudEvents, A2A, Kafka testing, no auth); never edit them here.
- `docs/adr/`: decisions (index: `docs/adr/index.md`). `apis/openapi.yaml`,
  `apis/asyncapi.yaml`: contracts. Operating docs: `docs/docs/operations/`
  (configuration, runbook, observability, troubleshooting),
  `docs/docs/development/testing.md`, `docs/docs/overview/quickstart.md`.
  `HARNESS.md`: what each sensor runs and why. `INCIDENTS.md`: the fleet
  incident log behind the fitness tests.

harness-template: v4
(hooks, guide-lint).

<!-- harness:scoped-rules:start (generated by tools/migrate_v3.py in warehouse-harness-template; do not hand-edit) -->
## Scoped rules and harness

Claude Code loads each rule below automatically when you touch the matching paths. OpenCode and Codex do NOT: read the rule BEFORE editing matching files.

| When touching | Read |
|---|---|
| `internal/adapters/**/kafka/**`, `apis/asyncapi*` | `.claude/rules/integration-events.md` |
| `internal/adapters/inbound/http/**`, `apis/openapi*.yaml`, `apis/openapi/**` | `.claude/rules/rest-api.md` |

Hooks (`scripts/harness/hook.py`, wired for Claude Code, Codex and OpenCode) block pushes to develop/main, `--no-verify`, bare `rm -rf`, and edits to generated files, and feed gofmt/vet findings back after each edit. Before saying "done" run `make check-fast`; the full gate is `make check-all`. `HARNESS_OFF=1` disables the hooks when debugging the harness itself.
<!-- harness:scoped-rules:end -->
