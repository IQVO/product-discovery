---
name: how-to-add-an-integration-event
description: Consume a new cross-service Kafka event in this service (it publishes nothing): CloudEvents 1.0 type naming, the consumed-event AsyncAPI note, the fold rule, the inbound consumer and its guard. Use when touching internal/adapters kafka code, a Consumer.Apply method, or apis/asyncapi*.yaml.
---

# How to add an integration event

product-discovery publishes nothing and consumes eleven events (ADR 0003, ADR 0004).
The envelope is CloudEvents 1.0 only; read `.claude/rules/integration-events.md` and
`.claude/rules/fleet/cloudevents.md` first. The worked example is
`ItemPriceWithdrawn`: guard in `internal/domain/productview/fold_price.go`
(`ApplyPriceWithdrawn`), use case `Consumer.ApplyItemPriceWithdrawn`
(`internal/application/usecases/consume_catalogue.go`), handler in
`internal/adapters/inbound/kafka/pricing.go`.

## Consuming a new event

1. **Fold rule.** Add the `Apply...` method on `productview.View` (or `categoryview`):
   decide the per-key guard (what makes a replay or an out-of-order fact `Stale`), return
   `shared.ErrInvalidFact` for a fact that can never be folded, and unit-test every branch
   in the domain package (mutation gates it).
2. **Use case.** Add `Consumer.Apply<Event>` next to its siblings: `c.fold(ctx, m, itemID, rule)`
   claims the CloudEvents id in the inbox and saves the row in ONE `ports.UnitOfWork`.
3. **Handler.** Register the full `type` string in the topic's handler table
   (`offer_availability.go`, `product_content.go` or `pricing.go`); decode `data` with
   `decodeData`, pass the result through `classify`. Add the type to the dispatch test
   (`pipeline_test.go`).
4. **Contract, one commit.** Add the message and its `data` schema (the fields you read) to
   `apis/asyncapi.yaml` and a row to the consumed table of
   `docs/adr/0003-consumed-contracts-and-rebuild.md`. Lint:
   `spectral lint apis/asyncapi.yaml --ruleset .spectral.asyncapi.yaml`.
5. **Snapshot.** If the producer's paged read carries the same fact, fold it in
   `usecases.Rebuilder` (`rebuild.go`) so the guard is seeded.
6. **Prove it.** `go test ./internal/adapters/inbound/kafka/ ./internal/application/...`, then
   `go test -tags=integration ./cmd/api/ -race -count=1` (real Kafka and Postgres, Docker needed).

A consumer group is configuration, never a literal; a new topic adds a
`*_CONSUMER_GROUP` variable in `cmd/api/config.go` and `workers.go`.

## If this service ever publishes

Do not add a second pattern here: instantiate the template's outbox (`how-to-add-an-integration-event`
of a publishing sibling such as product-content), add `type:` lines to ADR 0004 with or before the
AsyncAPI message (`TestEventCatalogueMatchesContract`), and write an ADR.
