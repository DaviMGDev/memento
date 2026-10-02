---
type: spec
title: "Spatiotemporal Composability for Go — Specification"
description: "A Go runtime for dynamic composition: revertible effects, reactive coeffects, and a declarative component loader."
tags: [spec]
sections: [context, users, user-stories, architecture, semantics, concurrency, configuration, conformance, nfr, versioning, non-goals, decisions]
created: "2026-10-02"
updated: "2026-10-02"
---

# Spatiotemporal Composability for Go — Specification

*This spec declares its own contract: every `##` section below appears in `sections:` in the frontmatter, and nothing appears undeclared. `context` and `decisions` are mandatory; the rest fit this project's kind (runtime library).*

## Context

Modern systems compose at runtime: plugin hosts, multi-tenant servers, and self-evolving agent harnesses load, unload, and reconfigure components while serving traffic. Two guarantees are missing from mainstream runtimes:

- **Temporal composability** — removing a component completely reverts the effects it installed on the shared environment.
- **Spatial composability** — components declare their dependencies and the runtime resolves, provides, and withdraws them reactively, activating and deactivating components as availability changes.

The paradigm this project implements reifies both guarantees as runtime mechanisms: every context mutation carries an inverse the runtime accumulates (revertible effects), and every context change is classified against each component's dependency specification to drive its lifecycle (reactive coeffects). The formal foundation is "A Programming Paradigm for Spatiotemporal Composability" (Shi, Zhang, Cui, 2026); Cordis is its reference implementation in TypeScript.

This project is an independent Go implementation of the same paradigm, not a Cordis port:

- preserved: revertible effects with LIFO recovery, reactive coeffect resolution by provider identity, dependency ordering, acyclic precedence, deterministic quiescence;
- replaced: Proxy-mediated access becomes an explicit typed API; runtime module loading and hot replacement become instance reload through a component registry;
- deferred: realm-based isolation, interception metadata, value-level reactivity, out-of-process components.

Goals: instance-level dynamic composition for Go services; complete reclamation of tracked effects; reactive dependency topology; deterministic convergence under reconfiguration; a behavioral conformance suite.

Stakeholders: component authors, application orchestrators, runtime maintainers.

## Users

**Component author** — writes a Go component: a typed configuration, its declarations (keys it injects and provides), and an activation body that installs effects through the context. Expects unload to be complete without writing an uninstall path.

**Application orchestrator** — assembles components from a declarative configuration and revises it at runtime: enabling, disabling, reconfiguring, replacing providers. Expects revisions to be incremental and safe.

**Runtime maintainer** — extends the runtime, owns the conformance suite, and evolves the key and versioning policy.

## User Stories

Story: US-001 — Effects are reclaimed on unload

As a component author,
I want every mutation I make through the context to be tracked with its inverse,
so that unloading my component restores the environment without hand-written cleanup.

Acceptance criteria (EARS):

- WHEN a component binds a key THE runtime SHALL register the inverse that unbinds it. (Event-driven)
- WHILE a component is unloading THE runtime SHALL apply its inverses in LIFO order. (State-driven)
- IF activation fails or panics THEN THE runtime SHALL roll back the effects already installed and mark the component failed. (Unwanted behavior)
- The runtime SHALL revert each tracked effect exactly once, even if unload is requested repeatedly. (Ubiquitous)

Story: US-002 — Dependencies drive activation

As a component author,
I want to declare the keys I need and have activation wait for them,
so that my activation body never reads an absent binding.

Acceptance criteria (EARS):

- WHILE a declared key has no active provider THE component SHALL stay inactive. (State-driven)
- WHEN every declared key becomes satisfied THE runtime SHALL activate the component. (Event-driven)
- WHEN a declared key's provider is replaced THE runtime SHALL deactivate and reactivate the component against the new provider. (Event-driven)
- IF loading a component would create a dependency cycle THEN THE runtime SHALL refuse the load with a descriptive error. (Unwanted behavior)

Story: US-003 — Teardown can still read its dependencies

As a component author,
I want my declared keys to remain readable while my component tears down,
so that closing a connection or releasing a handle can talk to the provider that is going away.

Acceptance criteria (EARS):

- WHEN a provider begins withdrawing THE runtime SHALL deactivate its dependents first. (Event-driven)
- WHILE a dependent is unloading THE runtime SHALL keep its declared bindings readable through its committed view. (State-driven)
- The runtime SHALL withdraw the provider's bindings only after every dependent has reached inactive. (Ubiquitous)

Story: US-004 — Reconfiguration is incremental and convergent

As an application orchestrator,
I want to revise a declarative configuration and have only the affected components change,
so that live state elsewhere is preserved and the system settles deterministically.

Acceptance criteria (EARS):

- WHEN an entry's configuration payload changes THE runtime SHALL reload only that component. (Event-driven)
- WHEN an entry is disabled THE runtime SHALL unload its component and leave its siblings active. (Event-driven)
- WHEN reconciliation completes THE runtime SHALL be quiescent, and the state SHALL match a from-scratch load of the final configuration up to declared equivalence. (Ubiquitous)

Story: US-005 — Custom resources get inverses too

As a component author,
I want an escape hatch to register an inverse for a resource the runtime does not manage,
so that my own acquisitions are reclaimed with the same guarantees.

Acceptance criteria (EARS):

- WHEN a component registers an explicit effect and its inverse THE runtime SHALL track and revert it like a built-in operation. (Event-driven)
- IF a component's resource cannot be reverted exactly THEN the runtime SHALL revert it up to the key's declared equivalence. (Unwanted behavior)

## Architecture

**Context tree.** Every component runs against a context derived from its parent's. A context carries a reference to its parent, its fiber (the instance identity), its own bindings, and views over inherited ones. Derived contexts are cheap; isolation is achieved by deriving.

**Typed keys.** A key is a statically typed, package-level value. Reads and writes are compile-time typed; the runtime table is heterogeneous and keyed by key identity. Keys are the unit of both declaration (inject/provide) and resolution.

**Bindings.** A binding is a key-value pair installed by a provider fiber. At most one active provider binds a key in a given resolution scope. Provider identity, not the bound value, is what dependents observe: replacing a provider is a change; overwriting a value in place is not.

**Fibers.** A fiber is one instance of a component: its declarations, its configuration, its lifecycle state, its accumulator of inverses, and its committed view — the map from each declared key to the provider it resolved when it activated.

**Registry and control plane.** The registry holds all live fibers. A single scheduler owns the registry, the per-key subscriber index, and target recomputation; component code runs in workers.

**Key flows.**

- *Load*: the orchestrator inserts an entry; the scheduler creates a fiber; when declarations are satisfied the fiber activates, installing effects through the context; each effect's inverse accumulates on the fiber.
- *Dependency change*: a binding is installed or withdrawn; the scheduler notifies subscribers; each subscriber recomputes its target view; a changed target schedules deactivation and/or activation.
- *Unload*: retirement flips the target; the fiber stops providing, dependents deactivate first, then the fiber applies its accumulator in LIFO order.

**Packages (suggested split).** `context` (tree, keys, bindings, effect registration), `runtime` (fibers, scheduler, lifecycle, ordering), `loader` (entries, component registry, reconciliation), `conformance` (property-test harness), with the feature files as the behavioral suite.

## Semantics

### Revertible effects

A context-mutating operation returns an error and registers its inverse with the calling fiber. The inverse is a synchronous function; the runtime invokes inverses serially in LIFO order when the fiber unloads. Recovery is idempotent and exactly-once.

A component may register an explicit effect: a callback that installs a custom resource and returns the inverse. The callback's obligation is the paradigm's effect witness: the returned inverse must restore the context state it was applied to, up to the key's declared equivalence.

Activation failures: an error or panic during activation stops the activation, rolls back the effects already installed, and lands the fiber in FAILED. A failed fiber is not retried automatically; re-enablement or reconfiguration is a revision that inserts a fresh fiber.

### Reactive coeffects

A component declares an inject set and a provide set of typed keys. A component is *satisfied* when every injected key has an active provider; the scheduler activates it only then. Providers bind by installing a binding for a provided key during activation; the binding is withdrawn by the provider's inverse.

Every binding change is classified against each dependent's declaration: gains activate, losses deactivate, neutral changes do nothing. Classification is by provider identity and by the declared key set, not by bound value.

### Lifecycle

States: INACTIVE, LOADING, ACTIVE, UNLOADING, FAILED. Only ACTIVE contributes bindings to resolution. LOADING and UNLOADING are transitional; a fiber in either provides nothing — its withdrawal is decided before its inverses run — but still resolves its own declared keys through its committed view.

Transitions are serialized per fiber and inertial: an in-flight activation completes before a target change is honored; the runtime then compares the target to the committed view and chains into deactivation if they differ, and symmetrically for deactivation into activation.

### Ordering and cycles

Precedence is defined by provide/inject overlap between fibers. The relation must be acyclic; inserting a fiber that would close a cycle is refused with an error naming the cycle. A provider's withdrawal is deferred until every dependent that resolved one of its keys reaches INACTIVE. Consequently a dependent's teardown always finds its declared bindings readable.

### Equivalence

Every recovery claim holds up to an observational equivalence declared per key. The default is deep equality; a key whose operations treat distinct representations as equivalent (handle renamings, pooled resources) supplies a comparator. Effects and bindings at other keys, and any state the runtime does not reify as a key, are outside the claim.

## Concurrency

- A single scheduler processes registry events — insert, retire, binding change, transition completion — sequentially. It never blocks on component code.
- Component code (activation bodies, inverses) runs on worker goroutines; completion is reported back to the scheduler as an event.
- Per-fiber transitions are serialized: at most one activation or deactivation per fiber at a time.
- Ordering waits are structural: the wait graph follows dependency precedence, which is acyclic by construction, so drain waits cannot deadlock.
- Undo functions may block; the runtime waits for them. A component that needs concurrent teardown may run goroutines inside one undo, as the runtime's serial execution orders the undo functions themselves.
- The runtime is safe for concurrent use; data races are excluded by ownership — the scheduler owns shared state — and checked in CI with the race detector.

## Configuration

A configuration is a tree of entries. An entry declares a stable id (the reconciliation key), the component it instantiates (a registry reference), a configuration payload, an enabled/disabled flag, and optional isolation annotation. Entries are declarative: they describe the desired composition, not the operations to reach it.

A component registry maps references to factories. Compiled Go cannot import modules by URL; the registry is populated at link time, or by extension packages for out-of-process components.

Reconciliation diffs the desired entry tree against live fibers:

- identity or component change → unload and re-instantiate;
- payload change → hand the new payload to the component, which decides whether a reload is material;
- enabled/disabled → unload or load;
- unrelated entries → untouched.

Guarantee: once reconciliation completes, the system is quiescent and equivalent — up to declared equivalence — to a from-scratch load of the final configuration.

## Conformance

The behavioral contract is specified as Gherkin features:

- `features/effects.feature` — registration, LIFO recovery, failure rollback, idempotence, external emissions.
- `features/coeffects.feature` — satisfaction, activation and deactivation, provider replacement versus in-place overwrite, teardown readability, cycle refusal.
- `features/lifecycle.feature` — state transitions, inertia and chaining, failure terminality, instance independence.
- `features/configuration.feature` — entries, per-field reconciliation, convergence to from-scratch state.

Property tests, shipped as a harness, cover the two witness obligations:

1. effect witness — for every effect applied at a state, applying its inverse returns an equivalent state;
2. coeffect commutativity — for every pair of operations at a key, both application orders reach equivalent states and yield equal outcomes.

Additional invariants: LIFO reversal of any effect sequence returns the start state; concurrent reconfiguration schedules reach the same quiescent state; the race detector is clean.

## NFR

- **Performance**: effect registration is allocation-light — one closure per effect; notification is O(affected subscribers); the scheduler adds no lock contention on the hot read path.
- **Portability**: Go 1.23+; no cgo; no code generation required in v1.
- **Observability**: the registry is introspectable — fiber states, committed views, and binding ownership are queryable for diagnostics.
- **Security posture**: access is declaration-based — a component sees the keys it declares; the runtime is not a sandbox, and untrusted code requires a process or embedder boundary.
- **Determinism**: the quiescent state is a function of the final configuration; repeated reconciliation is idempotent.

## Versioning

Keys are the compatibility surface. A key's value type and declared operations are its interface; changing them is a breaking change, and carrying a version in the key's identity (namespacing) is recommended when independent ecosystems share a registry. Configuration payloads are versioned by the component that defines them. Extension points — realm tables, interception, out-of-process components — are additive and must not change the semantics above.

## Non-Goals

- Code hot replacement: no in-process module reload; edited code requires a rebuild and process-level restart, or a component hosted out-of-process.
- Transparent property access: no Proxy-like mediation; access is explicit and typed.
- Cross-process composition in v1: a documented seam exists, but distribution, RPC, and network failure mapping are out of scope.
- Sandboxing: the runtime does not isolate untrusted code; it only constrains which dependencies a component declares.
- Value-level reactivity: no signals; reactivity is at component and dependency granularity.
- Realm-based isolation, interception metadata, and observational equivalence generation beyond per-key comparators.

## Decisions

- D1 — Instance-level scope; code hot replacement is a non-goal. Trade-off: compiled-language realism versus Cordis's hot-module reach.
- D2 — Per-operation undo registration with an explicit-effect escape hatch; recovery is LIFO, idempotent, exactly-once; failure rolls back to FAILED. Trade-off: ergonomic ordinary Go versus custom resources needing the escape hatch.
- D3 — Typed keys; inject/provide declarations; provider-identity change detection; per-key subscriber index. Trade-off: precise reactivity versus bookkeeping.
- D4 — Dependents deactivate before withdrawal; teardown reads through the committed view; acyclic precedence checked at insert. Trade-off: safe teardown versus stricter load acceptance.
- D5 — Single scheduler control plane with workers. Trade-off: determinism and race-freedom versus a coordination bottleneck, mitigated by workers.
- D6 — Declarative loader with per-field reconciliation and from-scratch convergence. Trade-off: more code paths versus preserving live state.
- D7 — Witness obligations enforced by a shipped property-test harness. Trade-off: zero runtime cost versus correctness not enforced in production.
- D8 — Module and package naming: `github.com/DaviMGDev/memento`, with packages `context`, `runtime`, `loader`, `conformance`. Trade-off: the `context` and `runtime` package names shadow the standard library's, requiring import aliases where both are used, against vocabulary fidelity to the paradigm.

Resolved since drafting: isolation model — single shared realm with tree shadowing for v1, realm tables deferred as an additive extension; verification level — contract plus the shipped property-test harness; naming — D8. No open points remain for v1.
