# memento

An independent Go implementation of **spatiotemporal composability** — a runtime for dynamic composition with two guarantees mainstream runtimes leave out:

- **Temporal composability** — removing a component completely reverts the effects it installed on the shared environment.
- **Spatial composability** — components declare their dependencies, and the runtime resolves, provides, and withdraws them reactively, activating and deactivating components as availability changes.

The behavioral contract lives in [`specs/`](specs/): [`SPEC.md`](specs/SPEC.md) is the specification, and [`specs/features/`](specs/features/) holds the Gherkin conformance suite.

## Quickstart

Requires Go 1.23+.

```console
$ go run ./examples/composition
-- apply: database and console
database: bound storage to postgres://db
console: connected to postgres://db
-- reconfigure: database payload changed
console: disconnected from postgres://db
database: bound storage to postgres://new
console: connected to postgres://new
-- disable: console
console: disconnected from postgres://new
-- unload: everything
fibers: 0
```

The example ([`examples/composition`](examples/composition)) assembles two components from a declarative configuration, reloads one of them on a payload change, disables the other, and unloads everything — with no hand-written teardown: every mutation registered its inverse, and unload replayed them.

## Packages

| Package | What it holds |
|---|---|
| [`context`](context) | Typed keys, the context tree with nearest-binding shadowing, provider-identified bindings, revertible effects (LIFO, exactly-once), the explicit-effect escape hatch, and per-key observational equivalence. |
| [`runtime`](runtime) | Fibers, the single-scheduler control plane with worker execution, reactive coeffects (satisfaction, provider-identity classification, per-key subscriber index), lifecycle with inertia, dependent-before-provider ordering, acyclic precedence, failure semantics, and registry introspection. |
| [`loader`](loader) | The declarative entry tree, the component factory registry, and per-field reconciliation that converges to the from-scratch state. |
| [`conformance`](conformance) | The executable behavioral suite — the Godog runner for `specs/features/*.feature` — and the shipped property harness for the two witness obligations. |

## Testing

```console
$ go test ./... -race
```

That runs the unit suites, all 31 Gherkin scenarios via Godog, and the property harness (effect witness, coeffect commutativity, LIFO reversal, randomized reconciliation convergence) under the race detector. CI runs the same command.

## Scope

v1 is instance-level dynamic composition. Code hot replacement, transparent Proxy-style access, cross-process composition, sandboxing, value-level reactivity, and realm tables are explicit non-goals — see [`specs/SPEC.md`](specs/SPEC.md) and its Decisions section.

The paradigm's formal foundation is "A Programming Paradigm for Spatiotemporal Composability" (Shi, Zhang, Cui, 2026); Cordis is its TypeScript reference implementation. This project is an independent Go implementation of the same paradigm, not a Cordis port.
