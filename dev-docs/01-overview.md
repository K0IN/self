# 01 · Overview

## Goal

- Local AI model server in Go.
- One command: `self serve decider-vision:2b`.
- First milestone: **decision models only** (System One: `choice`, `score`, `noul`).

## Scope

- In: decision models, GGUF files, text + one image, CUDA / Vulkan / CPU.
- Out (later): image gen, TTS, STT, LLM chat, embeddings.
- Out: non-GGUF formats, localhost HTTP between server and engine.

## Architecture

```
HTTP (chi) -> api/decision -> decision.Service -> Scheduler -> Adapter -> engine subprocess
```

- Go server owns HTTP, validation, images, queue.
- Native engine runs as a child process. Talks over stdin/stdout.
- Adapter = the only Go code that knows an engine's protocol.
- Registry (YAML) maps a model id to files + adapter. CLI references may add a
	quant suffix: `self serve decider-vision:2b@4bit`.

## Packages

| Package | Job |
| :--- | :--- |
| `cmd/self` | CLI |
| `cmd/registry-site` | Static site generator |
| `internal/config` | Flags > env > defaults |
| `internal/registry` | Parse + resolve registry |
| `internal/models` | Store, downloads, progress |
| `internal/runtime` | Find + run engine process |
| `internal/ipc` | `SELFIPC1` framing |
| `internal/ggufmeta` | GGUF header reading (abrander/gguf) |
| `internal/decision` | Types, adapter interface, scheduler, service |
| `internal/adapters/*` | Engine adapters |
| `internal/imageutil` | Image fetch/decode/resize |
| `internal/api` | Shared HTTP router, errors, health and model routes |
| `internal/api/decision` | Decision-model HTTP handlers |
| `internal/api/image` | Reserved for future image-model HTTP handlers |
| `internal/app` | `serve` and `check` wiring |
| `internal/onboard` | HF repo -> registry entry |
| `internal/site` | Registry -> HTML |
| `engines/ggmlc-custom-decider` | C++ engine |

## Acceptance criteria

- Must: `self serve <model>` downloads, starts the engine, serves HTTP.
- Must: `go test ./...` and `go vet ./...` pass.
- Must: no model-specific code outside adapters and the registry.
