# Development documentation

This directory describes the architecture and the workflows for developing,
testing, packaging, and extending `self`. User documentation lives in `docs/`
(published with VitePress); these files are for contributors.

Start with [01 · Overview](01-overview.md), then use:

- [02 · CLI](02-cli.md) for commands, flags, and configuration.
- [05 · Runtime and IPC](05-runtime-ipc.md) for engine discovery and the
  `SELFIPC1` protocol.
- [08 · HTTP API](08-http-api.md) for routes, request/response contracts, and
  errors.
- [13 · Build tooling](13-build-tooling.md) for `just` recipes, engine builds,
  container images, and the documentation site.
- [14 · Testing](14-testing.md) for unit tests, the end-to-end model tests, and
  smoke checks.

Each file also records how the feature works and its acceptance criteria.

| File | Feature |
| :--- | :--- |
| [01-overview.md](01-overview.md) | Goal, scope, architecture |
| [02-cli.md](02-cli.md) | `self` commands and config |
| [03-registry.md](03-registry.md) | Model registry (YAML) |
| [04-downloads.md](04-downloads.md) | Model store and downloads |
| [05-runtime-ipc.md](05-runtime-ipc.md) | Engine subprocess and IPC |
| [06-adapters.md](06-adapters.md) | Adapters (`ggmlc-laya`, `ggmlc-custom-decider`) and settings |
| [07-decider-engine.md](07-decider-engine.md) | Our C++ Decider engine |
| [08-http-api.md](08-http-api.md) | HTTP API and errors |
| [09-scheduling.md](09-scheduling.md) | Queue, cancellation, shutdown |
| [10-images.md](10-images.md) | Image input pipeline |
| [11-onboarding.md](11-onboarding.md) | Adding models, `self check` |
| [12-registry-site.md](12-registry-site.md) | Documentation and registry site (GitHub Pages) |
| [13-build-tooling.md](13-build-tooling.md) | `just`, engine build, container images |
| [14-testing.md](14-testing.md) | Tests and how to run them |
| [15-known-limits.md](15-known-limits.md) | Limits and open points |

Conventions:

- "Must" = acceptance criterion. Checked by a test or a manual step.
- Paths are relative to the repo root.
