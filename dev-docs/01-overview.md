# 01 · Overview

## Goal

- Local AI model server in Go.
- One command: `self serve decider-vision:2b`.
- Model types: decision models (System One: `choice`, `score`, `noul`) and
  text-to-speech (`audio`).

## Design principles

These decide every new feature. When in doubt, pick the option that keeps
them.

1. **Engines: plain upstream, as unchanged as possible.**
   - Best case: an existing upstream engine release, used as published
     (download the release binary, e.g. upstream Laya). No fork, no patches.
   - If upstream has no persistent server mode for the model, write the
     thinnest possible worker that links the **unmodified upstream release
     libraries** and only adds the SELFIPC1 loop (e.g. `ggmlc-custom-decider`
     and `ggmlc-audio` on llama.cpp's `libllama`/`libmtmd`). Model logic
     (architectures, tokenizers, codecs, sampling helpers) stays upstream.
   - The model is loaded once at start and stays loaded. A one-shot CLI that
     reloads the model per request is not an engine.
   - When upstream cannot do something, do not patch it in: record it in 15
     and wait for (or contribute) the upstream change.
2. **Public API: an established standard, never a home-grown one.**
   - Decisions use the System One API; audio (and later text, embeddings, STT)
     use the OpenAI API: same paths, fields, defaults and error shape. The
     official SDKs must work unchanged against `self`.
   - Extensions are additive only (a superset): new optional fields or
     endpoints, never a changed meaning of a standard field.
   - A standard feature the engine cannot do returns an explicit error
     (`unsupported_capability`, 422); it is never silently ignored.
3. **Models live in the registry.** Every servable model is a registry entry
   (`models/registry.yml`) with pinned files (`size` + `sha256`), preferably
   from the upstream publisher's Hugging Face repo. Everything a model needs at
   run time (projector, default voice) is a pinned registry file too. Adding a
   model of a supported family is a registry entry and a model card, no code.
4. **Stateless requests.** A request carries all its data (e.g. a reference
   voice as bytes). No filesystem paths in the API, no server-side profiles or
   IDs, no request data written to disk.

## Scope

- In: decision models (text + images), text-to-speech, OpenAI-compatible chat
  completions, GGUF files, CUDA / Vulkan / CPU.
- Out (later): image generation and STT. A new model type slots in without
  touching the shared code, see "Adding a model type" in 06.
- Out: non-GGUF model formats and public exposure of the engine's private
  HTTP endpoint.

## Architecture

```
HTTP (chi) -> model API -> typed service -> Adapter -> engine subprocess
```

- Go server owns HTTP, validation, images, queue.
- Native engines run as child processes. Decision/audio workers use SELFIPC1;
  text models use the unmodified upstream `llama-server` HTTP API over a private
  Unix domain socket (`--host` set to its path), with private loopback TCP on
  Windows. The HTTP client uses Unix `DialContext`; the socket's `0700`
  temporary directory is removed after the child exits. Public embeddings
  are not implemented yet.
- The adapter is the only Go code that knows an engine's protocol.
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
| `internal/audio` | Audio types and adapter interface |
| `internal/adapters/*` | Engine adapters |
| `internal/settings` | Typed engine settings schema |
| `internal/localconf` | Local `settings.yml` |
| `internal/imageutil` | Image fetch/decode/resize |
| `internal/errs` | Typed error kinds shared by all layers |
| `internal/jsonx` | Ordered JSON objects |
| `internal/api` | Shared HTTP router, errors, health and model routes |
| `internal/api/decision` | Decision-model HTTP handlers |
| `internal/api/audio` | OpenAI-compatible speech HTTP handlers |
| `internal/api/text` | OpenAI-compatible chat completion HTTP handlers |
| `internal/api/image` | Reserved for future image-model HTTP handlers |
| `internal/app` | `serve`, `pull`, `check`, `benchmark`, `settings` wiring: shared `target` + HTTP lifecycle, one file per model type |
| `internal/onboard` | HF repo -> registry entry (library only, no CLI command yet) |
| `models` | `registry.yml` and model cards (data only) |
| `engines/ggmlc-custom-decider` | C++ engine |
| `engines/ggmlc-audio` | C++ text-to-speech engine (same llama.cpp libraries) |
| `llama-server` | Upstream llama.cpp HTTP engine for chat models |
| `engines/clef` | C++ engine for Clef Flash (backbone via llama.cpp + native joint head) |
| `engines/laya` | Recipe that fetches the upstream Laya engine |
| `tests/models` | Source registry checks and end-to-end model tests |
| `docs` | VitePress site: user docs + registry pages |

## Acceptance criteria

- Must: `self serve <model>` downloads, starts the engine, serves HTTP.
- Must: `go test ./...` and `go vet ./...` pass.
- Must: no model-specific code outside adapters and the registry.
- Must: the design principles above hold for every engine, endpoint and model.
