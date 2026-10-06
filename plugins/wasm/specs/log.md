---
type: log
title: "plugins/wasm/specs/ log"
description: "Activity log for the WASM loader plugin specification"
created: "2026-10-05"
updated: "2026-10-06"
---

# plugins/wasm/specs/ log

## 2026-10-06

- Implemented the host-services ABI: `HostServices` injected through
  `WithHostServices`, the seven imports wired and registered, cancellation
  gating on status-bearing action imports, and `features/host-services.feature`
  as the normative scenarios, executed by `services_test.go` (stashed read-back,
  caller attribution, unset decline, declaration-probe refusal, publish, and
  cancellation). Refs: core-agent#6.
- Specified the host-services ABI (D11-D13): `job_start`/`job_peep`/`job_kill`
  route opaque request documents to an injected `HostServices` contract and
  stash one result document (`job_result_len`/`job_result`), so a large result
  costs a second read and `job_start` never waits on a job; `publish` reaches
  the host bus; `cancel_poll` answers for the calling job only, and a killed
  caller's host imports fail with the canceled code (`2`). Additive: existing
  exports and guests are untouched; unconfigured services decline cleanly.
  Refs: core-agent#6.

## 2026-10-05

- Initialized `plugins/wasm/specs/` — the loader had no isolated specification, against
  `AGENTS.md`'s convention that every baked-in plugin carries its own. Adopted the root
  `specs/` shape: `SPEC.md`, `features/*.feature`, `index.md`, `log.md`.
- Specified the paper-faithful registration and value surface (D1-D7): `bind` as the
  provider's tracked, revertible set; `get`/`get_len` as dependent reads resolved through
  the committed view; `invoke` as host-mediated routing to the provider's
  `memento_alloc`/`memento_handle`; declared provides must be bound or activation fails.
  References: arXiv:2608.25512 §3.2 (Def. 23-26, eq. 22-23).
- Implemented the extended ABI — `bind`/`get`/`invoke` in `plugins/wasm`, with
  `memento_alloc`/`memento_handle` routing, activation failure for unbound
  declared provides, and hand-built module tests (`bindings_test.go`,
  `invoke_test.go`). The `echo` and `chat` examples now bind their provides;
  `go vet ./...` and `go test ./... -race` are green. Status moved to
  implemented.
- Added the host-mediated HTTP transport (D8-D9): `http_request`,
  `http_response_len`, and `http_response` on the loader's host module, with an
  `Engine`-owned `Egress` policy (`WithHTTPAllowHosts`, `WithHTTPTimeout`,
  `WithHTTPTransport`). The response is stashed rather than caller-buffered, so
  a short read never repeats the request; declaration probes carry no egress,
  keeping declarations pure. Covered by `transport_test.go` and
  `features/transport.feature`.
- Added host-side credential references (D10): header values carry `env:NAME`
  and the host resolves them through `WithHTTPCredentialResolver`, so a guest
  authenticates without holding a secret and an unavailable reference fails
  the exchange.
