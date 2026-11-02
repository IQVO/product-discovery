# BLOCKED: savepoint fix not applicable to product-discovery

The task ports the Idempotency-Key transaction middleware fix, but this repo has no such middleware to fix.

Evidence (develop @ a918333):
- `git grep -l 'Idempotency-Key' -- '*.go'` matches only a doc comment in `internal/adapters/inbound/http/server.go:5`: "no write route, hence no Idempotency-Key".
- The service is a read-only projection: three read routes (GET /products, /products/{id}, /categories) plus liveness/readiness. No POST/PATCH route exists in the router or `apis/openapi.yaml`.
- No `RequireIdempotencyKey`, no `idempotency_keys` table, no ctx-bound `pgx.Tx` binder, no idempotency integration test. Writes happen only in the Kafka consumer's UnitOfWork, which has no HTTP outcome to record.

The invariant ("a mapped 4xx over an aborted tx is recorded and returned; a 2xx over an aborted tx is never returned or cached") has no code path here to hold or violate.

Writing the middleware just to patch it would add route/middleware behaviour beyond the savepoint fix, which the task forbids. It would also contradict the documented design.

Suggested follow-up: drop product-discovery from the savepoint-fix rollout. If a write route is ever added, adopt the fleet middleware with the savepoint (slotting-optimization 45e3714) from the start.
