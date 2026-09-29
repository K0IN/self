# 07 · Decider engine (`engines/ggmlc-custom-decider`)

## Why

- Decider GGUFs are llama.cpp files (`qwen35`, separate mmproj).
- Upstream laya can't run them and has no image input.

## What

- `main.cpp` (~600 lines C++). SELFIPC1 on stdio.
- Links **prebuilt** llama.cpp release libs (b11256): `libllama`, `libmtmd`, ggml backends.
- No llama.cpp compile. No CUDA toolkit needed.
- Vision via `libmtmd` (Qwen-VL resize, M-RoPE). Marker `<__media__>`.

## Readout (mirrors Mapika/decider)

- Prompt: `Context:\n<state>\n\nQuestion k: …\nOptions:\n(A) …\nAnswer k: (`.
- One forward pass. Logits at each `" ("` answer slot.
- Softmax over letters `A…J`.
- `noul` = P(yes).
- `score` = one yes/no row per level ("isolated levels"), normalised.
- Confidence formulas follow upstream `systemone.py`.

## Ready frame

- Capabilities: text, choice, score, noul, `max_options: 10`.
- Vision: `max_images: 1`, bounded 1536×1536, resize `contain`.

## Build

- `just engine-decider` (= `just engine build`), see 13.
- Output: `bin/libexec/ai-server/ggmlc-custom-decider` + libs in `lib/`.

## Numbers (RTX 4050, 2b-vision Q4_K_M)

- Load ~4 s. First vision request ~1.3 s (warmup).
- Vision, 1 image: 50–400 ms (image size).
- Text, 4 questions: ~67 ms.

## Acceptance criteria

- Must: `self check decider:2b-vision` passes all probes (verified).
- Must: vision examples answer correctly (verified): lake 99.7%, coyote 96.1%, cactus 99.4%, pig 99.2%.
- Must: 11 options -> 422. 2 images -> 422.
- Must: 8 concurrent requests all 200.
- Must: bad request -> error frame, engine keeps running.
- Open: `self check` for `decider:0.8b` and `decider:4b` not run yet.
