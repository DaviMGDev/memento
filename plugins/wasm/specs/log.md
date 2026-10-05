---
type: log
title: "plugins/wasm/specs/ log"
description: "Activity log for the WASM loader plugin specification"
created: "2026-10-05"
updated: "2026-10-05"
---

# plugins/wasm/specs/ log

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
