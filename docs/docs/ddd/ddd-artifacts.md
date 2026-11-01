---
id: ddd-artifacts
title: DDD artifacts (ddd-crew)
sidebar_label: DDD artifacts index
sidebar_position: 10
---

# DDD artifacts (ddd-crew)

The strategic and tactical design of `product-discovery`, drawn with the
[ddd-crew](https://github.com/ddd-crew) tools plus UML and ER diagrams. Every diagram is Mermaid, derived from
the code and the ADRs on this branch, and carries a `Source:` line naming the files it came from. The context
has **no aggregate and publishes no event**, so the aggregate canvas is applied to its two projections and the
event pages describe consumed events only.

| Artifact | ddd-crew tool / notation | What it shows |
| --- | --- | --- |
| [Core domain chart](/docs/ddd/core-domain-chart) | [Core Domain Charts](https://github.com/ddd-crew/core-domain-charts) | Where the context sits on business differentiation versus model complexity, and why it is Supporting |
| [Bounded context canvas](/docs/ddd/bounded-context-canvas) | [Bounded Context Canvas v5](https://github.com/ddd-crew/bounded-context-canvas) | Purpose, classification, roles, every inbound and outbound message, decisions, metrics |
| [Context map](/docs/ddd/context-map) | [Context Mapping](https://github.com/ddd-crew/context-mapping) | The context's slice of the storefront map, with the pattern of every edge |
| [Aggregate design canvas](/docs/ddd/aggregate-design-canvas) | [Aggregate Design Canvas v1.1](https://github.com/ddd-crew/aggregate-design-canvas) | No aggregate by decision: the canvas applied to ProductView and CategoryView |
| [Domain message flow](/docs/ddd/domain-message-flow) | [Domain Message Flow Modelling](https://github.com/ddd-crew/domain-message-flow-modelling) | Search, the three feeds and the rebuild as numbered flows |
| [EventStorming](/docs/ddd/eventstorming) | [EventStorming glossary and cheat sheet](https://github.com/ddd-crew/eventstorming-glossary-cheat-sheet) | Process-level boards with the hotspots and how the code resolved them |
| [Ubiquitous language](/docs/ddd/ubiquitous-language) | Glossary | Every term with the code identifier that implements it |
| [Class diagram](/docs/ddd/class-diagram) | UML class diagram | Domain types, the ports and the adapters that exist |
| [Entity relationship](/docs/ddd/entity-relationship) | ER diagram | The Postgres schema of migration `0001_product_discovery_schema` |
| [Sequence diagrams](/docs/ddd/sequence-diagrams) | UML sequence diagram | Search, get product, list categories, a fold, a dead letter and the rebuild |
| [Domain events](/docs/ddd/domain-events) | Event catalogue | The eleven consumed CloudEvents with topic, key, fields and guards |

Related pages outside the pack: [Use cases](/docs/ddd/use-cases),
[Subdomain classification](/docs/ddd/subdomain-classification), [Integration](/docs/ecosystem/integration)
and the [event catalogue](/docs/api-reference/events).

:::note Sources of truth
The code wins. Domain rules come from `internal/domain/productview`, `categoryview` and `search`, behaviour
from `internal/application/usecases`, the envelope from `internal/adapters/kafka/cloudevents`, the schema
from `internal/adapters/outbound/postgres/migrations/`, the REST contract from `apis/openapi.yaml`, the
event contract from `apis/asyncapi.yaml` and decisions from the ADRs in `docs/adr/`. Where a page and the code
disagree, the page is wrong. The central documentation site holds an earlier, model-derived version of these
pages under
[warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/contexts/product-discovery).
:::

:::info Not in this pack
The A2A agent ([ADR 0007](/docs/adr/0007-a2a-inbound-adapter)) is a second inbound adapter over the same read
use cases and adds no aggregate, entity or event, so it has no page of its own; it appears on the context map,
the canvas, the class diagram and the sequence diagrams. There is no MCP server and no analytics read side.
:::
