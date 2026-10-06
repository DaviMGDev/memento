---
type: spec
title: "WASM Component Loader — Specification"
description: "Runs capability code as WebAssembly guests on wazero: declarations, paper-faithful registration, value reads, and invocation, wired to the kernel's effects and coeffects."
tags: [spec, plugin]
sections: [context, abi, semantics, invocation, transport, host-services, lifecycle, conformance, non-goals, decisions]
created: "2026-10-05"
updated: "2026-10-06"
---

# WASM Component Loader — Specification

*This spec declares its own contract: every `##` section below appears in
`sections:` in the frontmatter, and nothing appears undeclared. It is the isolated
specification of the `plugins/wasm` baked-in plugin; the kernel's behavioral
contract stays in the root [`specs/`](../../specs/SPEC.md) and is never modified
for plugin reasons.*

## Context

`plugins/wasm` runs capability code as WebAssembly guests on
[wazero](https://github.com/tetratelabs/wazero), wired to the kernel through a
`runtime.Component` adapter. It is what lets a component arrive at runtime, not
only at build time.

Two kernel guarantees are the reason the loader exists in this shape:

- **Temporal composability** — every mutation a guest makes through the host
  ABI is a revertible effect owned by the guest's fiber; unloading replays the
  inverses, so no guest writes a teardown path.
- **Spatial composability** — guests declare the keys they inject and provide;
  the kernel activates a guest only once its dependencies are satisfied,
  deactivates dependents first when a provider withdraws, and refuses duplicate
  providers and cycles.

The paradigm's registration is `set(k, v)` — an effect on the coeffect context
that the provider performs and the runtime tracks and reverts
(arXiv:2608.25512 §3.2, Def. 23). A declaration alone registers nothing:
satisfaction is `k ∈ dom(σ)` (Def. 24), and the value at a key carries the
operations a dependent may invoke (Def. 24, eq. 22-23). The ABI below makes
those three notions — declare, register, invoke — explicit.

Guests are compiled from Go via the `wasip1` port (`-buildmode=c-shared`
produces a WASI reactor) or from any language targeting core wasm with the
imports below. WASI `snapshot_preview1` is instantiated for wasi guests, and the
reactor initialization export (`_initialize` or `_rt0_wasm_wasip1_lib`) is
called after instantiation when present.

Stakeholders: guest authors, runtime maintainers, and hosts embedding the
loader (for example core-agent's `cmd/` entries).

## ABI

Strings cross as `(pointer, length)` pairs into the guest's exported memory;
every host import returns `0` on success or a non-zero error code, except the
length/copy imports, which return a byte count with `0` meaning
absent/too-small/error.

Guest exports:

| Export | Signature | Purpose |
|---|---|---|
| `memento_declare` | `() -> i32` | declares injected and provided keys |
| `memento_activate` | `() -> i32` | activation body: binds provided values, registers effects |
| `memento_revert_effect` | `(i32) -> i32` | inverse of an effect registered by id |
| `memento_alloc` | `(i32) -> i32` | provider-side buffer for `invoke` (optional) |
| `memento_handle` | `(i32,i32,i32,i32) -> i32` | provider-side operation handler (optional) |
| `memory` | — | linear memory |

Host imports:

| Import | Signature | Purpose |
|---|---|---|
| `memento.declare_inject` | `(i32,i32) -> i32` | declare an injected key |
| `memento.declare_provide` | `(i32,i32) -> i32` | declare a provided key |
| `memento.bind` | `(i32,i32,i32,i32) -> i32` | register `value` at `key` (the paper's `set`) |
| `memento.get_len` | `(i32,i32) -> i32` | length of the value resolved for `key` |
| `memento.get` | `(i32,i32,i32,i32) -> i32` | copy the value resolved for `key` |
| `memento.invoke` | `(i32,i32,i32,i32,i32,i32) -> i32` | invoke an operation of the value at `key` |
| `memento.http_request` | `(i32,i32) -> i32` | perform one HTTP exchange for a JSON request document |
| `memento.http_response_len` | `() -> i32` | length of the stashed response document |
| `memento.http_response` | `(i32,i32) -> i32` | copy the stashed response document |
| `memento.job_start` | `(i32,i32) -> i32` | start a job from a JSON request document; stash the result document |
| `memento.job_peep` | `(i32,i32) -> i32` | read one job's status from a JSON request document; stash the result document |
| `memento.job_kill` | `(i32,i32) -> i32` | kill one job from a JSON request document; stash the result document |
| `memento.job_result_len` | `() -> i32` | length of the stashed job result document |
| `memento.job_result` | `(i32,i32) -> i32` | copy the stashed job result document |
| `memento.publish` | `(i32,i32,i32,i32) -> i32` | publish an event (topic, payload) to the host bus |
| `memento.cancel_poll` | `() -> i32` | ask whether the calling job was killed |
| `memento.get_payload_len` | `() -> i32` | length of the activation payload |
| `memento.get_payload` | `(i32,i32) -> i32` | copy the activation payload |
| `memento.register_effect` | `(i32) -> i32` | register an opaque effect id whose inverse is `memento_revert_effect` |
| `memento.log` | `(i32,i32) -> i32` | write to the host log writer |

Key names are resolved through one `KeyRegistry` per engine, so every component
of an engine agrees on key identity. The registry's default key type is
`Key[any]`; values crossing this ABI are byte slices.

## Semantics

**Declarations.** `memento_declare` runs once on a probe instance and the
injected/provided key sets become the component's declarations. The kernel
enforces its structural rules on them: one provider per key, no inject/provide
overlap, no cycles.

**Registration.** During activation the guest calls `memento.bind(key, value)`
for each declared provided key. The host installs the value through the
kernel's `Bind`, so the registration is a fiber-owned revertible effect: the
inverse that withdraws it is tracked with the guest's other effects and runs in
LIFO order on unload. The kernel advertises a key only once its fiber owns the
binding, and provider identity — not the value — is what dependents observe.

A declared provide left unbound fails activation with a descriptive error:
declaring a provide is a promise that activation registers it. Bound values are
byte slices; a binding installed by other means (typed Go components) is
visible to the kernel but not readable through `get`.

**Reads.** A dependent reads an injected key with `memento.get_len` and
`memento.get`. Resolution goes through the instance's committed view, so a
dependent keeps reading the same provider during its own teardown after the
provider has begun withdrawing — the kernel's teardown-readability guarantee.
`get` copies at most `buf_max` bytes and returns the count copied.

**Reactions.** When a provider withdraws or is replaced, the kernel deactivates
its dependents first and only then runs the provider's inverses. A replacement
provider is a fresh activation with a fresh module instance and a fresh
registration; the old registration is withdrawn with the old module's effects.

## Invocation

`memento.invoke(key, req)` routes one request to the provider of `key`. Only a
fiber that declared `key` injected may invoke, and the key must be provided and
its module alive.

The host performs the exchange entirely in the provider's memory:

1. call the provider's `memento_alloc(req_len)` and `memento_alloc(resp_max)`;
2. copy the request bytes into the first buffer;
3. call `memento_handle(req_ptr, req_len, resp_ptr, resp_max)` — the handler may
   call any host import, attributed to the provider's own instance (its `log`,
   `get`, and nested `invoke` calls see the provider's context);
4. copy the response into the caller's buffer and return its length.

A provider without `memento_alloc`/`memento_handle`, an absent binding, a
non-dependent caller, or an oversized handler response yields `0` bytes: the
invocation fails without a transition. Cyclic invocations are impossible by the
kernel's cycle refusal, and provider calls are serialized with the same module
lock that guards activation.

Invocation is the call surface the paradigm's operations need (Def. 24): the
value bound at a key is the capability, and `memento_handle` implements the
operations that act on it. Values remain plain bytes; live Go values are not
crossed.

## Transport

Guests reach the network only through the host. A guest writes a JSON request
document `{method, url, headers, body}` into its memory and calls
`memento.http_request`; the host performs one exchange under its egress policy
and stashes a JSON response document `{status, headers, body}` that the guest
reads back with `memento.http_response_len` and `memento.http_response`. The
call returns `0` on a completed exchange — any status, including an error
status — and non-zero on a policy or transport failure. Bodies are text; binary
payloads are out of scope for this ABI.

`http_request` takes the request alone: the response is stashed on the instance
rather than written into a caller buffer, so a body larger than the guest
anticipated costs a second read, never a second request. A later `http_request`
replaces the stash. Timeouts, allow-lists, and the transport itself are the
host's: the `Engine` carries an `Egress` policy whose zero value is permissive
(any host, a default timeout, `net/http`), and hosts constrain it with
`WithHTTPAllowHosts`, `WithHTTPTimeout`, and `WithHTTPTransport`. A failure is
non-fatal to the guest: the code returns and a diagnostic line is written to
the configured log writer.

A header value may carry a credential reference — `env:NAME`, optionally
embedded, as in `Bearer env:OPENAI_API_KEY`. When the host configures a
credential resolver (`WithHTTPCredentialResolver`), the host substitutes the
secret before the request leaves; an unavailable reference fails the exchange
rather than crossing the wire literally. Without a resolver, values pass
through unchanged. The guest therefore holds a reference and never a secret.

Reads require an active instance. Declaration runs on a probe with no egress,
so a request from `memento_declare` fails: declarations describe the component,
they do not act.

## Host Services

Guests reach host-owned jobs and events through imports that route to a
`HostServices` contract the embedding host injects (`WithHostServices`). The
loader treats every document as opaque bytes: the service parses the request,
owns the semantics, and returns the result document.

Job calls carry one JSON request document — the shape of a tool call is the
service's concern — and stash one JSON result document, read back with
`job_result_len` and `job_result`, exactly like the HTTP response pair. A
result larger than the guest anticipated costs a second read, never a second
call; each job call replaces the instance's stash. `job_start` returns as soon
as the service has produced the result document; it never waits for the job to
finish.

`publish` delivers one event to the host bus. Topic and payload cross as
bytes; queueing, delivery, and subscriber wakes belong to the host.

`cancel_poll` answers whether the host killed the job the caller is executing:
`0` means keep going, the canceled code means unwind at the next safe point.
The host answers for the calling job only. While a caller's job is killed,
every subsequent host import fails with the canceled code except `log`
(best-effort) and `cancel_poll` itself, which reports the canceled code as its
positive answer — so a killed call unwinds at its next host import even if it
never polls. Error codes are shared: `0` success, `1` failure, `2` canceled.

Services are optional and injected. With no host services configured, job and
publish imports fail with code `1` and `cancel_poll` answers `0`, so an engine
without a host stays usable. Declaration runs on a probe with no instance:
job and publish imports fail from `memento_declare`, like `http_request`.

## Lifecycle

One module instance per activation. The host instantiates the compiled module,
initializes the reactor when present, registers the module-close cleanup as the
bottom-most effect, and calls `memento_activate`. Returning non-zero, panicking,
or failing to bind a declared provide fails the activation and the kernel rolls
back installed effects. Worker execution is serialized per instance by a module
lock, so a handler call never interleaves with its provider's own activation or
teardown.

## Conformance

The Gherkin scenarios in [`features/`](features/) are normative.
Executable conformance lives in the package's own test suite
(`abi_test.go`, `component_test.go`, `integration_test.go`), run with
`go test ./... -race`; cross-component flows are additionally exercised by
`examples/` guests.

## Non-Goals

- Transport policy beyond one exchange: credential resolution, proxies,
  retries, redirects, streaming, and binary bodies are the caller's concern;
  the host performs one request and returns one response.
- Isolation or sandboxing of guests: guests run with the host's authority.
- Hot replacement of running instances, transparent proxy access to guest
  memory, value-level reactivity, and cross-process composition.
- A guest-to-guest direct call surface: all routing goes through the host.

## Decisions

- **D1 — Registration is a tracked effect.** `bind` is the paper's `set`,
  installed through the kernel's revertible-effect machinery on the provider's
  fiber; it is the only way a wasm guest registers a provide.
- **D2 — Declared means registered.** Activation fails when a declared provide
  was not bound; declaration alone advertises nothing.
- **D3 — Values are bytes.** The ABI value type is `[]byte`; keys default to
  `Key[any]`, and non-byte bindings are invisible to `get`.
- **D4 — Reads use the committed view.** `get` resolves exactly like the
  kernel's typed reads, preserving teardown readability and provider-identity
  semantics.
- **D5 — Invocation is host-mediated.** Providers export
  `memento_alloc`/`memento_handle`; the host owns routing, buffers, locking,
  and failure codes. There is no direct guest-to-guest channel.
- **D6 — Additive ABI.** The existing imports keep their signatures and
  behavior; bind/get/invoke extend the surface, so a guest that binds nothing
  and never invokes still works as before (except D2, which is the point).
- **D7 — Isolated specification.** This spec and its features are the plugin's
  contract; the loader never reaches into or edits the root specs.
- **D8 — Transport is host-mediated.** Guests reach the network only through
  `http_request`; the host performs the exchange, owns the egress policy, and
  reports policy and transport failures as non-zero codes. A guest never holds
  a socket.
- **D9 — The response is stashed, not buffered by the caller.** `http_request`
  takes the request alone, and the response is read back with the
  `http_response_len`/`http_response` pair, mirroring
  `get_payload_len`/`get_payload`; a short read retries the copy, never the
  request.
- **D10 — Credentials are host-substituted references.** Request header values
  carry `env:NAME` references; the host resolves them through its configured
  resolver, and an unavailable reference fails the exchange. Secrets never
  enter guest memory, and the kernel stays free of a particular secret store.
- **D11 — Host services are injected, never built in.** The job, publish, and
  cancel imports route to a `HostServices` contract supplied through
  `WithHostServices`; the loader passes opaque bytes and the caller's instance
  and owns nothing about tools, topics, or job records. Unconfigured services
  decline: job and publish calls fail, `cancel_poll` answers `0`.
- **D12 — Job results are stashed documents.** `job_start`/`job_peep`/`job_kill`
  take one request document and stash one result document — read back with
  `job_result_len`/`job_result` — so a large result costs a second read, never
  a second call, and `job_start` returns without waiting for the job.
- **D13 — Cancellation is a poll and a code.** While the calling job is killed,
  every host import except `log` fails with the canceled code (`2`), so a call
  unwinds at its next host import even if it never polls; `cancel_poll` reports
  the same code as its positive answer, for the calling job only.
