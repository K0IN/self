# 01 · Overview

## Goal

- Local AI model server in Go.
- One command: `self serve decider-vision:2b`.
- First milestone: **decision models only** (System One: `choice`, `score`, `noul`).

## Scope

- In: decision models, GGUF files, text + one image, CUDA / Vulkan / CPU.
- Out (later): image gen, TTS, STT, LLM chat, embeddings. A new model type
  slots in without touching the shared code, see "Adding a model type" in 06.
- Out: non-GGUF formats, localhost HTTP between server and engine.

## Architecture

```
HTTP (chi) -> api/decision -> decision.Service -> Scheduler -> Adapter -> engine subprocess
```

- Go server owns HTTP, validation, images, queue.
- Native engine runs as a child process. Talks over stdin/stdout.
- Adapter = the only Go code that knows an engine's protocol.
- Registry (YAML) maps a model id to files + adapter. CLI references may add a
	quant suffix: `self serve decider-vision:2b@q4`.
- The registry is published as `https://k0in.github.io/self/models.yml` (built
  from `models/registry.yml`, see 12). `self` fetches it at run time and keeps
  an offline copy in the models directory (see 03).
- `tests/models` can start the real `self serve` per model and check the API
  end to end (see 14).

## Packages

| Package | Job |
| :--- | :--- |
| `cmd/self` | CLI (`main.go`: serve/check/benchmark/settings; `models.go`: pull/ls/ls-remote/rm) |
| `internal/config` | Flags > env > defaults |
| `internal/registry` | Parse + resolve registry, fetch the published document |
| `internal/models` | Store, downloads, progress, registry cache |
| `internal/runtime` | Find + run + supervise engine process |
| `internal/ipc` | `SELFIPC1` framing |
| `internal/ggufmeta` | GGUF header reading (abrander/gguf) |
| `internal/decision` | Types, adapter interface, scheduler, service |
| `internal/adapters/*` | Engine adapters |
| `internal/settings` | Typed engine settings schema |
| `internal/localconf` | Local `settings.yml` |
| `internal/imageutil` | Image fetch/decode/resize |
| `internal/errs` | Typed error kinds shared by all layers |
| `internal/jsonx` | Ordered JSON objects |
| `internal/api` | Shared HTTP router, errors, health and model routes |
| `internal/api/decision` | Decision-model HTTP handlers |
| `internal/api/image` | Reserved for future image-model HTTP handlers |
| `internal/app` | `serve`, `pull`, `check`, `benchmark`, `settings` wiring: shared `target` + HTTP lifecycle, one file per model type |
| `internal/onboard` | HF repo -> registry entry (library only, no CLI command yet) |
| `models` | `registry.yml` and model cards (data only) |
| `engines/ggmlc-custom-decider` | C++ engine |
| `engines/laya` | Recipe that fetches the upstream Laya engine |
| `tests/models` | Source registry checks and end-to-end model tests |
| `docs` | VitePress site: user docs + registry pages |

## Acceptance criteria

- Must: `self serve <model>` downloads, starts the engine, serves HTTP.
- Must: `go test ./...` and `go vet ./...` pass.
- Must: no model-specific code outside adapters and the registry.
