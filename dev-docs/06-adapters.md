# 06 · Adapters

## Interface (`internal/decision`)

- `Start`, `Info` (capabilities), `Decide`, `Close`, `Done`.
- Typed. No `Run(any)`.
- Registered in `internal/adapters/registry.go` (name -> engine + factory).

## `ggmlc-laya`

- Engine: upstream `laya daemon` from monatis/ggmlc v0.9.6 (`just runtime`).
- Protocol: NDJSON on stdio. Ready line `{"status":"ready","model":"laya"}`.
- Ready line has no capabilities -> read from GGUF metadata (`internal/ggufmeta`).
- Runs ggmlc-compiled GGUFs only (`ggmlc.graph_spec` + `ggmlc.decision` / `laya.*`).
- Upstream aborts (SIGABRT) with > 16 options -> adapter enforces `max_options` first.
- Rejects llama.cpp GGUFs (e.g. `qwen35`) with `unsupported_model`.
- Text only.

## `ggmlc-custom-decider`

- Engine: our C++ engine (see 07).
- Go side: generic `internal/adapters/selfipc` + a small spec (`internal/adapters/customdecider`).
- Spec: args `--model <m> --device <d> [--mmproj <p>]`, model check, timeout.
- Capabilities come from the engine's `ready` frame.

## Settings (`internal/settings`)

- Each adapter has a typed schema: name, flag, type, bounds / enum.
- Order: registry model < registry quant < local adapter < local model < local quant < `--set`.
- Local file: `~/.ai-server/settings.yml` (optional). Override: `--settings-file` / `AI_SERVER_SETTINGS` (must exist).
  - Package `internal/localconf`. Strict YAML (typos fail). Example: `examples/settings.yml`.
- `self settings <id>`: value + source per key, plus unset keys with help.
- Validated before the engine starts. Bad key/type/range -> `invalid_request`.
- Rendered as engine flags. False bools emit nothing.
- Printed at startup (`Settings …`) and in `GET /v1/model`.

| Adapter | Keys |
| :--- | :--- |
| `ggmlc-custom-decider` | `context_size`, `threads`, `temperature`, `gpu_layers`, `flash_attn`, `image_min_tokens`, `image_max_tokens` |
| `ggmlc-laya` | `threads`, `cuda_graph` (context is compiled into the GGUF) |

## Adding an engine

1. Engine speaks SELFIPC1 -> write a spec only (like `customdecider`).
2. Other protocol -> new package under `internal/adapters/`.
3. Add one line to `internal/adapters/registry.go`.

## Acceptance criteria

- Must: > max options -> 422 before the engine sees it (laya never crashes).
- Must: wrong GGUF type for adapter -> `unsupported_model` with a clear reason.
- Must: `selfipc` works against a fake engine (test binary doubles as engine via `SELFIPC_FAKE`).
- Must: laya integration test passes with a real engine (`just test-integration`).
- Must: settings precedence and rejection covered (`internal/adapters` + `internal/settings` tests).
- Must: local layers sit between registry and `--set`; sources reported (`TestResolveSettingsLocal`).
- Must: missing default file is fine; missing explicit file errors; bad local values error with the file path.
- Manual: `self check decider:2b-vision --settings-file …` uses local values (verified).
- Manual: `self check decider:2b-vision --set context_size=4096 --set flash_attn=on` passes (verified).
- Manual: `--set top_k=5` and laya `--set context_size=…` rejected with the list of known keys (verified).
