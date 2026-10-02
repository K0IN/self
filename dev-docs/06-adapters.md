# 06 · Adapters

## Interface (`internal/decision`)

- `Start`, `Info` (capabilities + image geometry), `Decide`, `Close`, `Done`.
- Typed. No `Run(any)`.
- Registered in `internal/adapters/registry.go` (name -> engine executable + factory + settings schema).
  `adapters.Lookup(type, name)` returns the type-independent part (`Base`: engine + settings schema),
  which is all `self settings`, settings resolution and engine lookup need.
- Every adapter checks question types and option counts against the engine
  capabilities before it writes to the engine.
- Process handling is shared: an adapter calls `runtime.Start`, wraps it with `runtime.Supervise`, then uses
  `runtime.Handshake` (first message, or start error with exit status + stderr tail, killed on cancel),
  `Supervisor.Await` (one answer under a watchdog), `CrashErr` and `Close`. The adapter owns only its protocol.

## `ggmlc-laya`

- Engine: upstream `laya daemon` from monatis/ggmlc (`GGMLC_VERSION`, default v0.9.6), installed by `just runtime` / `just setup`.
- Protocol: NDJSON on stdio. Ready line `{"status":"ready","model":"laya"}`.
- Ready line has no capabilities -> read from GGUF metadata (`internal/ggufmeta`).
- Runs ggmlc-compiled GGUFs only (`ggmlc.graph_spec` + `ggmlc.decision` / `laya.*`).
- Option limit is baked into the GGUF (`ggmlc.decision` `max_opts`, else `laya.max_opts` / `kev.max_opts`, default 16).
  Upstream aborts (SIGABRT) on overflow -> the adapter enforces the limit first (and the registry `info.max_options`).
- Rejects llama.cpp GGUFs (e.g. `qwen35`) with `unsupported_model`. Also rejects a model that lists an `mmproj` file.
- Text only.
- A request without an answer after 2 minutes kills the engine (`runtime_crashed`).

## `ggmlc-custom-decider`

- Engine: our C++ engine (see 07).
- Go side: generic `internal/adapters/selfipc` + a small spec (`internal/adapters/customdecider`).
- Spec: args `--model <m> --device <d> [--mmproj <p>]` + rendered settings, model check, 2 minute request timeout (default of `selfipc.Spec`).
- Model check before start: the GGUF has an architecture and an embedded tokenizer, is not a ggmlc graph (-> use `ggmlc-laya`) and not a `clip` projector; an mmproj file must be a `clip` GGUF.
- Capabilities come from the engine's `ready` frame.

## `ggmlc-audio` (`internal/adapters/audio`)

- Engine: our C++ worker `engines/ggmlc-audio/main.cpp`, linked against the same
  prebuilt llama.cpp `libllama`, `libmtmd` and GGML libraries as the Decider
  engine (`engines/ggmlc-custom-decider/deps`).
- Registry adapter name: `ggmlc-audio`; model files use the `model` and `mmproj`
  roles (the projector is required, `Start` fails with `unsupported_model`
  without it).
- Lifecycle, same as `ggmlc-custom-decider`: `Start` runs
  `runtime.Start` + `runtime.Supervise` + `runtime.Handshake`. The worker loads
  the model, context and projector once and then sends
  `{"type":"ready","protocol":1,"device":…,"audio":{"pipeline":"qwen3tts","sample_rate":24000}}`.
  `Synthesize` sends one SELFIPC1 request and waits with `Supervisor.Await`
  (5 minute timeout, then the worker is killed: `runtime_crashed`). `Close`
  closes stdin and waits for the exit.
- Request: `{"id":N,"method":"synthesize","params":{"input","language","speaker_attachment":0}}`.
  The reference voice (MP3/WAV bytes) is attachment 0 and is decoded in memory
  with `mtmd_helper_bitmap_init_from_buf`. No path is ever sent.
- Default voice: when the registry variant has a `voice` file, `Start` reads it
  into memory once and `Synthesize` sends it whenever a request has no voice
  (Pocket TTS produces almost no audio without one). The engine stays generic:
  it only ever sees an attachment.
- Per request the worker clears the KV cache and creates a fresh
  `mtmd_helper_gen_audio` state and sampler chain. The model stays loaded.
- Response: `{"id":N,"result":{"sample_rate","frames","samples","latency_ms"}}`
  with the WAV as attachment 0; the adapter checks the `RIFF` magic. An engine
  error frame `{"error":{"type":"invalid_request",…}}` maps to 400, other types
  to `internal_error` (500). The worker survives a failed request.
- Settings: `context_size` (`--ctx`), `threads`, `gpu_layers`, `temperature`,
  `top_p`, `top_k`, `frames` (`--max-frames`), `seed`.
- Requests run one at a time (scheduler queue); parallel HTTP requests wait.
- Tests: `internal/adapters/audio/adapter_test.go` uses the test binary as a
  fake engine and checks that two requests are answered by the same process.
- Reference voice data is request-scoped and stateless. No filesystem path or
  server-side voice profile belongs in the API or adapter contract.
- OpenAI speech fields remain accepted at HTTP; unsupported runtime features
  such as non-WAV output, SSE, instructions, and non-default speed must return
  `unsupported_capability` rather than being silently ignored.

## `clef`

- Engine: `engines/clef` (C++, see below). Go side: `selfipc` + `internal/adapters/clef`.
- Spec: args `--model <m> --head <h> --device <d> [--mmproj <p>]` + settings. The `head` role file is `joint_head.safetensors`.
- The engine ports Cloudflare's `JointSchemaHead` (`head.hpp`, float32 CPU) and encodes requests like the reference
  `encode_record`: llama.cpp (+ mtmd for images) returns the last hidden state of every position, the head scores all
  options of all questions jointly. Answers: `choice` confidence = probability of the chosen option, `noul` confidence =
  larger of p and 1-p.
- Up to 32 options per question and 4 images. The state is truncated to fit `context_size` (as upstream does).
- Head parity: logits match the reference PyTorch head to ~1e-6 (float32) on random inputs.
- Resident for the life of the process: backbone, projector, head weights (float32, ~0.5 GB) and the
  mapped output embedding. Per request: clear the KV cache, evaluate the whole prompt in decodes of at most
  512 positions (llama.cpp also computes vocabulary logits for every output position, so this bounds
  memory), then run the head on the CPU. No prefix cache. The engine reports the device it really uses.

## Settings (`internal/settings`)

- Each adapter has a typed schema: name, flag, type, bounds / enum.
- Order: registry model < registry quant < local adapter < local model < local quant < `--set`.
- Local file: `~/.ai-server/settings.yml` (optional). Override: `--settings-file` / `AI_SERVER_SETTINGS` (must exist).
  - Package `internal/localconf`. Strict YAML (typos fail). Shape: `version: 1`, `adapters.<adapter>`, `models.<id>.settings`, `models.<id>.quants.<quant>`.
    Example: `docs/public/examples/settings.yml`.
- `self settings <id>`: value + source per key, plus unset keys with help.
- Validated before the engine starts. Bad key/type/range -> `invalid_request` (the message lists the known keys).
- `--set` values are strings and converted by the schema (`on`/`off`/`true`/`false` for bools).
- Rendered as engine flags in schema order. False bools emit nothing.
- Printed at startup (`Settings …`) and in `GET /v1/model`.

| Adapter | Keys |
| :--- | :--- |
| `ggmlc-custom-decider` | `context_size`, `threads`, `temperature`, `gpu_layers`, `flash_attn`, `image_min_tokens`, `image_max_tokens` |
| `clef` | `context_size`, `threads`, `gpu_layers` |
| `ggmlc-laya` | `threads`, `cuda_graph` (context is compiled into the GGUF; `cuda_graph` defaults to on for `auto` / `cuda` devices) |

Types, bounds, and defaults are in the schemas (`customdecider.Settings`,
`ggmlclaya.Settings`) and shown by `self settings <id>`. Keep the custom decider
schema in sync with the flags in `engines/ggmlc-custom-decider/main.cpp`.

## Adding an engine

1. Engine speaks SELFIPC1 -> write a spec only (like `customdecider`).
2. Other protocol -> new package under `internal/adapters/`.
3. Add one entry to `decisionAdapters` in `internal/adapters/registry.go`.

## Adding a model type (for example TTS)

Everything below is new code beside the decision code; nothing shared changes shape.

1. Registry: add the `ModelType` constant and one `modelTypes` entry (capabilities, file roles) in `internal/registry/types.go`; a new file role is a constant there.
2. Typed interface in `internal/<type>` like `internal/decision` (`RuntimeConfig`, `Adapter`, `Factory`). No shared adapter interface. Build the process on `runtime.Start` + `runtime.Supervise`; use `internal/ipc` framing when the engine speaks SELFIPC1.
3. `internal/adapters/registry.go`: an `<type>Adapters` table (entries embed `Base`) and one `case` in `Lookup`.
4. HTTP: `internal/api/<type>` with a `Mount(chi.Router) api.ModelInfo`. Router, `/health`, `/v1/model` come from `internal/api`.
5. `internal/app/<type>.go`: a `serve<Type>` that runs `resolveTarget` output through `locate`, `describe`, `download`, starts the engine and hands a `runningModel` to `serveHTTP`; plus one `case` in `Serve`. `models.Ensure` returns files by role (`models.Files`).
6. Engine under `engines/` with a `just` recipe.

For audio specifically, also add the persistent native worker, its CMake link
target, the SELFIPC1 request/response schema, attachment limits, ready-handshake
parser, and an integration test that proves two requests use one process.

No change: `pull`, `ls`, `ls-remote`, `rm`, settings layering, `self settings`, downloads and the store layout.
`check`, `benchmark` and `tests/models` are decision-only until the type gets its own probes and checks.

## Acceptance criteria

- Must: > max options -> 422 before the engine sees it (laya never crashes).
- Must: wrong GGUF type for adapter -> `unsupported_model` with a clear reason (`TestCheckModel` in `ggmlclaya` and `customdecider`).
- Must: `selfipc` works against a fake engine (test binary doubles as engine via `SELFIPC_FAKE`).
- Must: laya integration test passes with a real engine: `SELF_TEST_ENGINE=<laya> SELF_TEST_GGUF=<gguf> go test ./internal/adapters/ggmlclaya -run Integration` (optional `SELF_TEST_DEVICE`; skipped otherwise).
- Must: settings precedence and rejection covered (`TestResolveSettingsPrecedence`, `TestResolveSettingsRejects`).
- Must: local layers sit between registry and `--set`; sources reported (`TestResolveSettingsLocal`).
- Must: missing default file is fine; missing explicit file errors; bad local values error with the file path.
- Manual (verified): `self settings decider-vision:2b --set context_size=4096 --set flash_attn=on` reports both with source `--set`.
- Manual (verified): `--set top_k=5` and laya `--set context_size=…` are rejected with the list of known keys.
- Manual (verified): `--settings-file /nonexistent.yml` fails with the path.
