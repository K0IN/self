# Dev docs

Short docs per feature. Each file has: what it does, how it works, acceptance criteria.

| File | Feature |
| :--- | :--- |
| [01-overview.md](01-overview.md) | Goal, scope, architecture |
| [02-cli.md](02-cli.md) | `self` commands and config |
| [03-registry.md](03-registry.md) | Model registry (YAML) |
| [04-downloads.md](04-downloads.md) | Model store and downloads |
| [05-runtime-ipc.md](05-runtime-ipc.md) | Engine subprocess and IPC |
| [06-adapters.md](06-adapters.md) | Adapters (`ggmlc-laya`, `ggmlc-custom-decider`) |
| [07-decider-engine.md](07-decider-engine.md) | Our C++ Decider engine |
| [08-http-api.md](08-http-api.md) | HTTP API and errors |
| [09-scheduling.md](09-scheduling.md) | Queue, cancellation, shutdown |
| [10-images.md](10-images.md) | Image input pipeline |
| [11-onboarding.md](11-onboarding.md) | `self onboard` / `self check` |
| [12-registry-site.md](12-registry-site.md) | GitHub Pages site |
| [13-build-tooling.md](13-build-tooling.md) | `just`, engine build, release |
| [14-testing.md](14-testing.md) | Tests and how to run them |
| [15-known-limits.md](15-known-limits.md) | Limits and open points |

Conventions:

- "Must" = acceptance criterion. Checked by a test or a manual step.
- Paths are relative to the repo root.
