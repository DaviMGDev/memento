---
type: log
title: "specs/ log"
description: "Activity log for the specs/ directory"
created: "2026-10-02"
updated: "2026-10-02"
---

# specs/ log

## 2026-10-02

- Initialized `specs/` — Spatiotemporal Composability for Go, status draft. `SPEC.md` and the four conformance feature files were adopted from the closed discussion `golang-spatiotemporal-composability` (accepted proposal: core + loader scope, single realm with tree shadowing, contract + property-test harness). Project and module naming remain open.
- Implemented the runtime — module `github.com/DaviMGDev/memento` with `context`, `runtime`, `loader`, and `conformance`. All 31 scenarios in `features/*.feature` pass via Godog (strict mode), the effect-witness and coeffect-commutativity harness passes, randomized reconciliation converges to the from-scratch state, and `go test ./... -race` is green (CI: `.github/workflows/ci.yml`).
