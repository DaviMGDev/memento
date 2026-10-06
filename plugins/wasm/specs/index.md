---
type: index
title: "plugins/wasm/specs/ — Index"
description: "Node registry and reading order for the WASM loader plugin specification"
created: "2026-10-05"
updated: "2026-10-06"
---

# plugins/wasm/specs/ — Index

Project status: implemented

## Nodes

| File | Title | Description |
|------|-------|-------------|
| [SPEC.md](SPEC.md) | WASM Component Loader — Specification | The plugin contract: host ABI, registration, values, invocation, host services, module lifecycle, conformance, non-goals, decisions |
| [features/abi.feature](features/abi.feature) | WASM host ABI | Conformance scenarios: bind registration, unbound provides, value reads, invocation routing |
| [features/transport.feature](features/transport.feature) | WASM host HTTP transport | Conformance scenarios: host-performed exchanges, egress policy, timeouts, pure declarations |
| [features/host-services.feature](features/host-services.feature) | WASM host services | Conformance scenarios: stashed job documents, publish, cancellation poll and code, additive decline |
| [features/lifecycle.feature](features/lifecycle.feature) | WASM component lifecycle | Conformance scenarios: one instance per activation, reactor initialization, effect reversal |
| [log.md](log.md) | plugins/wasm/specs/ log | Activity log of spec changes |

Feature files are Gherkin-format citizens and carry no YAML frontmatter, since the
Gherkin grammar has no frontmatter construct.

## Reading order

1. [SPEC.md](SPEC.md) — the frontmatter `sections:` field is the contract; then read top to bottom.
2. `features/*.feature` — the behavioral conformance surface, linked from SPEC.md's Conformance section.
