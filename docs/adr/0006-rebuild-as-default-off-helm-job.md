# ADR 0006: The rebuild binary ships as an optional, default-off Helm Job

## Status

Accepted (2026-10-27).

## Context

This repository builds two binaries: `cmd/api` (the always-on service: REST plus the three Kafka consumers) and
`cmd/rebuild` (ADR 0003, gap S5: a one-shot ops job that seeds ProductView and CategoryView from the producers'
paged snapshot reads). A Helm chart that modelled `cmd/rebuild` as a second always-on Deployment would be wrong
twice over: the job would re-seed the projections on every pod restart, and its replica would sit idle consuming
resources between runs. A chart that omitted it entirely would push the runbook onto tribal knowledge.

## Decision

The chart renders `cmd/rebuild` as a `batch/v1` Job, `rebuild.enabled: false` by default, with
`restartPolicy: Never`, `backoffLimit: 1` and a TTL on the finished Job. Enabling it requires the three producer
base URLs (`rebuild.sources.*`); a missing one fails the render naming the value, mirroring the binary's own
boot check. The Job runs the same image as the api with `command: ["/app/rebuild"]` and reads `DATABASE_URL`
from the same Secret. It gets no `KAFKA_BROKERS`: it needs none — cmd/api's consumers resume from their
committed offsets afterwards and the per-source guards drop every replayed event that is not newer (ADR 0003).
The intended operation is: one `helm upgrade` with `rebuild.enabled=true`, watch the Job to completion, then
disable it again — or run the binary operator-side. Both are documented in `docs/operations/runbook.md`.

## Consequences

There is exactly one always-on workload (the api Deployment), so the Service-selects-one-Deployment invariant
stays trivially true. The rebuild path is versioned with the chart instead of living in a wiki, and enabling it
accidentally is loud (a rendered Job) rather than silent (a second Deployment re-seeding forever).
