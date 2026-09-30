# Onboarding new models

Goal: a new model is a **registry entry**, not new code. Code is only needed
when a model needs an engine or readout that does not exist yet.

```
  Hugging Face repo
        │  just onboard <owner/repo>        (reads only the GGUF header, a few MiB)
        ▼
  registry snippet  ──paste──►  models/registry.yml
        │  just check-model <name:tag>      (download, start engine, probe questions)
        ▼
  ok / FAIL with a reason
        │  just serve <name:tag>
        ▼
  POST /v1/systemone
```

## 1. Generate the entry

```bash
just onboard Mapika/decider-4b-GGUF
# or with an explicit id
just onboard mradermacher/decider-0.8b-GGUF --id decider:0.8b
```

`self onboard`:

- lists the repo (`/api/models/<repo>/tree/main`);
- groups GGUF files into `4bit` (Q4_K_M > Q4_K_S > IQ4_XS > Q4_0), `5bit`,
  `6bit`, `8bit` (Q8_0) and `f16` (F16/BF16); the first found is the default;
- attaches a multimodal projector (`*mmproj*`, prefers Q8_0) as `role: mmproj`;
- reads the model header over HTTP range requests and picks the adapter:

| GGUF metadata | Adapter | Engine |
| :--- | :--- | :--- |
| `ggmlc.graph_spec` + `ggmlc.decision` or `laya.*` | `ggmlc-laya` | upstream `laya daemon` |
| any llama.cpp architecture (`qwen35`, `qwen3`, `llama`, …) | `ggmlc-custom-decider` | `engines/ggmlc-custom-decider` |

- pins every file with its exact `size` and `sha256` (from the Hub's LFS
  metadata);
- writes a model card skeleton to `models/readmes/<name>/<tag>.md`
  (`--readme-dir ""` to skip) and links it with `readme:`.

Review the snippet (trim quants you don't want, replace the TODO
`description`), edit the model card, and paste the snippet into
`models/registry.yml`.

### Registry fields

| Field | Required | Meaning |
| :--- | :--- | :--- |
| `description` | yes | one line, shown by `self list` and on the overview page |
| `readme` | yes | path to the model card (Markdown), relative to `registry.yml`, e.g. `readmes/kev/4b.md` |
| `files[].size` | yes | exact byte size of the upstream file |
| `files[].sha256` | yes | sha256 of the upstream file |
| `info` | no | metadata: `family`, `parameters`, `architecture`, `base_model`, `source`, `license`, `languages`, `context_length`, `max_options`, `homepage` (prefilled by `self onboard`). `max_options` also caps requests. |
| `settings` | no | engine parameters (below); also allowed under a quant to override |

### Engine settings

Precedence (low to high):

1. registry model `settings`
2. registry quant `settings`
3. local file `adapters.<adapter>`
4. local file `models.<id>.settings`
5. local file `models.<id>.quants.<quant>`
6. `self serve <id> --set key=value`

The local file is `~/.ai-server/settings.yml` (optional), or
`--settings-file FILE` / `AI_SERVER_SETTINGS` (must exist). Example:
[`settings.yml`](/self/examples/settings.yml). `self settings <id>`
(`just settings <id>`) shows every key, its value and where it came from.
Unknown keys, wrong types and out-of-range values are rejected before the
engine starts. Effective settings are printed at startup and returned by
`GET /v1/model`.

`ggmlc-custom-decider`:

| Key | Type | Flag | Default |
| :--- | :--- | :--- | :--- |
| `context_size` | int 512–262144 | `--ctx` | 8192 |
| `threads` | int | `--threads` | llama.cpp default |
| `temperature` | float | `--temperature` | 1.0 |
| `gpu_layers` | int, -1 = all | `--gpu-layers` | -1 |
| `flash_attn` | auto / on / off | `--flash-attn` | auto |
| `image_min_tokens` | int | `--image-min-tokens` | from mmproj |
| `image_max_tokens` | int | `--image-max-tokens` | from mmproj |

`ggmlc-laya` (context and option limits are compiled into the GGUF):

| Key | Type | Flag | Default |
| :--- | :--- | :--- | :--- |
| `threads` | int | `--threads` | 4 |
| `cuda_graph` | bool | `--cuda-graph` | on for `auto`/`cuda` devices |

New settings: add a `settings.Param` to the adapter's schema
(`internal/adapters/<adapter>/…`) and the flag to the engine.

`size` + `sha256` are enforced on every download: if Hugging Face advertises
a different file (the repo was updated), `self` refuses to download it; if
the received bytes do not match, the file is deleted and never used. An
installed file whose size differs from the pin is re-downloaded. When a
repo is updated on purpose, re-run `self onboard` and take the new pins.

## 2. Verify it

```bash
just check-model decider:4b
```

`self check` downloads the default quant, starts the engine exactly like
`serve`, and sends five fixed probes (obvious routing, the same with shuffled
options, clearly-true / clearly-false noul, urgency ordering). A wrong prompt
format or readout fails loudly here instead of returning plausible garbage in
production:

```
  ok    choice: obvious routing      choice=billing  (301ms)
  ok    choice: option order swap    choice=billing  (23ms)
  ok    noul: clearly true           noul=0.985 (want > 0.6)  (19ms)
  ok    noul: clearly false          noul=0.002 (want < 0.4)  (19ms)
  ok    score: ordering              score=1.87 (want >= 1.5)  (44ms)
All probes passed. decider:4b is ready: self serve decider:4b
```

## 3. Serve it

```bash
just serve decider:4b
```

## When code *is* needed

| Situation | What to add |
| :--- | :--- |
| New GGUF of an existing family | nothing (registry entry) |
| New engine that speaks SELFIPC1 | one `selfipc.Spec` (command line + optional GGUF check) and one line in `internal/adapters/registry.go` |
| Engine with its own protocol | a package under `internal/adapters/<name>/` implementing `decision.Adapter` (see `ggmlclaya`) |
| New model *mode* (image gen, TTS, …) | a new typed interface (`image.Adapter`, …); registry, downloads, runtime, IPC are reused |

### SELFIPC1 engine contract

Any native executable can be an engine if it:

1. writes a `ready` frame first, listing its capabilities and image geometry;
2. answers `systemone` request frames (raw RGB images as attachments) with
   `result` or `error` frames carrying the same `id`;
3. writes only frames to stdout (logs to stderr) and exits when stdin closes.

The full message shapes are documented in
`internal/adapters/selfipc/adapter.go`; the fake engine in
`internal/adapters/selfipc/adapter_test.go` is a complete ~80-line example.

## Engines

### `ggmlc-laya` (upstream)
`laya daemon` from [monatis/ggmlc](https://github.com/monatis/ggmlc) releases,
fetched by `just runtime`. Runs ggmlc-compiled decision GGUFs (Kev, Laya).

### `ggmlc-custom-decider` (ours)
`engines/ggmlc-custom-decider/main.cpp` (~600 lines), built by
`just engine-decider`. It links the **prebuilt** llama.cpp release libraries
(`libllama`, `libmtmd`, ggml CUDA/CPU backends) — no llama.cpp compile, no
CUDA toolkit. It reproduces the Decider readout from
[Mapika/decider](https://github.com/Mapika/decider)
(`prompt.py`, `systemone.py`, `vision/model.py`):

- plain state-first prompt: `Context:\n<state>\n\nQuestion k: …\nOptions:\n(A) …\nAnswer k: (`;
- one `llama_decode`, logits only at the `" ("` answer slots, softmax over the
  option-letter tokens `A…J`;
- noul = P("yes"); score = isolated levels (one yes/no row per level,
  normalised), exactly as upstream `systemone.py`;
- images: `<__media__>` + prompt via `libmtmd` (Qwen-VL smart resize, M-RoPE),
  so the Go side only bounds the image size.

Limits of this first version: up to 10 options per question (the narrow
`(A)…(J)` rendering), one image per request, plain layout only
(`decider_config.json` `"layout": "chat"` models are not supported yet),
temperature 1.0 (per-model temperatures from `decider_config.json` can be
passed with `--temperature`).

#### Building the engine
The whole build lives in `engines/ggmlc-custom-decider/justfile`, imported
into the root justfile as the module `engine`:

```bash
just engine-decider                      # = just engine build (cuda-12.8)
just engine build vulkan                 # or cpu
just engine deps                         # only fetch llama.cpp libs + headers into deps/
just engine compile                      # only cmake + ninja into build/
just engine verify                       # ldd check of the installed bundle
just engine clean                        # drop build/ and deps/
just --list engine                       # all engine recipes
LLAMA_CPP_TAG=b11300 just engine build   # upgrade llama.cpp (deps/ is re-fetched when tag/variant change)
```

## Projects that helped / can help

| Project | How we use it / could use it |
| :--- | :--- |
| [ggml-org/llama.cpp](https://github.com/ggml-org/llama.cpp) | Our custom engine links its release `.so` files; `libmtmd` does all vision preprocessing. Supports hundreds of architectures, so most future llama.cpp-format decision models only need a registry entry. |
| [Mapika/decider](https://github.com/Mapika/decider) | Reference for the prompt, readout, confidences and score isolation; `engine_gguf.py` is the upstream llama.cpp readout we mirrored in C++. |
| [monatis/ggmlc](https://github.com/monatis/ggmlc) | Upstream Laya engine; also the path to compile other PyTorch decision models into self-describing GGUFs (`ggmlc.decision`). |
| [abrander/gguf](https://github.com/abrander/gguf) | GGUF header parsing (local and, via range requests, remote). |
| Hugging Face Hub API (`/api/models/<repo>/tree`, `?expand[]=gguf`) | Repo listing and GGUF summary for `self onboard`; `X-Linked-Size` / `X-Linked-Etag` for download verification. |
| [ollama](https://github.com/ollama/ollama) | Same shape (Go server + native runner subprocess + registry); good reference for multi-platform runner packaging and model manifests if we outgrow the YAML registry. |
| [LocalAI](https://github.com/mudler/LocalAI) | Go server with many gRPC backends per modality — reference for the future image/TTS/STT adapters. |
| [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp), [whisper.cpp](https://github.com/ggml-org/whisper.cpp) | ggml-based native engines for the planned image and STT modes; they fit the same subprocess + SELFIPC1 pattern. |
