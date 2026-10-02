---
type: index
title: "specs/ — Index"
description: "Node registry and reading order for the specs/ directory"
created: "2026-10-02"
updated: "2026-10-02"
---

# specs/ — Index

Project status: draft

## Nodes

| File | Title | Description |
|------|-------|-------------|
| [SPEC.md](SPEC.md) | Spatiotemporal Composability for Go — Specification | The spec monolith: context, users, stories, architecture, semantics, concurrency, configuration, conformance, NFR, versioning, non-goals, decisions |
| [features/effects.feature](features/effects.feature) | Revertible effects | Conformance scenarios: registration, LIFO recovery, failure rollback, idempotence, external emissions |
| [features/coeffects.feature](features/coeffects.feature) | Reactive coeffects | Conformance scenarios: satisfaction, activation and deactivation, provider identity, teardown readability, cycle refusal |
| [features/lifecycle.feature](features/lifecycle.feature) | Component lifecycle | Conformance scenarios: state transitions, inertia, chaining, failure terminality, instance independence |
| [features/configuration.feature](features/configuration.feature) | Declarative configuration | Conformance scenarios: entries, incremental reconciliation, convergence |
| [log.md](log.md) | specs/ log | Activity log of spec changes |

Feature files are Gherkin-format citizens and carry no YAML frontmatter, since the Gherkin grammar has no frontmatter construct. Decision records (`adr/`) are added when decisions outgrow SPEC.md's Decisions section.

## Reading order

1. [SPEC.md](SPEC.md) — the frontmatter `sections:` field is the contract; then read top to bottom.
2. `features/*.feature` — the behavioral conformance surface, linked from SPEC.md's Conformance section.
