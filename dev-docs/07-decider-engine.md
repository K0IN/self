# 07 · Decider engine (`engines/ggmlc-custom-decider`)

## Why

- Decider GGUFs are llama.cpp files (`qwen35`, separate mmproj).
- Upstream laya can't run them and has no image input.

## What

- `main.cpp` (single file, ~720 lines C++). SELFIPC1 on stdio.
- Links **prebuilt** llama.cpp release libs (`LLAMA_CPP_TAG`, default b11256): `libllama`, `libmtmd`, ggml backends.
- No llama.cpp compile. No CUDA toolkit needed.
- Backends are loaded at start from `lib/` next to the executable.
- Vision via `libmtmd` (Qwen-VL resize, M-RoPE). Marker `<__media__>`.
- Flags: `--model`, `--mmproj`, `--device`, `--ctx`, `--threads`, `--temperature`, `--gpu-layers`, `--flash-attn`, `--image-min-tokens`, `--image-max-tokens`, `--verbose`. `self` passes them from the adapter settings (see 06).

## Readout (mirrors Mapika/decider)

- Prompt: `Context:\n<state>\n\nQuestion k: …\nOptions:\n(A) …\nAnswer k: (`. The number `k` only appears with more than one row.
- State: a string is used as is; any other JSON is rendered like Python `json.dumps`, with `_index` added to arrays of 8 or more items.
- One forward pass. Logits at each `" ("` answer slot.
- Softmax over letters `A…J` (divided by `--temperature`).
- `noul` = P(yes).
- `score` = one yes/no row per level ("isolated levels"), normalised.
- Confidence formulas follow upstream `systemone.py`.

## Ready frame

- Capabilities: text, choice, score, noul, `max_options: 10`.
- Vision (only with an mmproj): `max_images: 1`, bounded 1536×1536, resize `contain`.

## Request limits and errors

- Choice: 1-10 options. Score: 2-10 levels. Noul: no criteria. Instructions required.
- More than one image, or an image on a text-only load -> `invalid_request` error frame (the Go side rejects these first with 422).
- Prompt longer than `--ctx` -> `invalid_request`.
- Bad request -> error frame, the engine keeps running. Unexpected failure -> `internal_error` error frame.
- Malformed frame, missing `id`, or unknown framing -> the engine exits with status 3.

## Build

- `just engine-decider [variant]` (= `just engine build [variant]`), see 13.
- Output: `bin/libexec/ai-server/ggmlc-custom-decider` + libs in `lib/`.

## Performance

Not tracked in the repository. Load time and latency depend on the model, the
device, and the llama.cpp backend; measure on the target machine with
`self serve` and the requests in 08.

## Acceptance criteria

- Must: `self check` passes all probes for `decider-vision:2b`, `decider:0.8b`, and `decider:4b` (verified on CPU).
- Must: vision answers are correct for solid red/green/blue images (end-to-end test `vision: solid colors`, see 14).
- Must: 11 options -> 422. 2 images -> 422 (end-to-end tests `limits` and `vision`).
- Must: 8 concurrent requests all 200 (end-to-end test `queue`).
- Must: bad request -> error frame, engine keeps running.
