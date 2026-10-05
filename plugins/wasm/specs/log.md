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
