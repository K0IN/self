# 11 · Onboarding new models

## Flow

```
write registry entry (size + sha256 from HF LFS metadata)  ->  add readme + description  ->  models/registry.yml  ->  go test ./...  ->  publish (12)  ->  self check <model>
```

The entry is written by hand using this document and the registry rules in 03.
`internal/onboard` can analyze a Hugging Face repo (quant buckets,
mmproj, adapter from the GGUF header, LFS pins) and render a YAML skeleton, but
no CLI command calls it yet.

## Registry entry

Format: see 03.

- Group GGUFs into quants (the `internal/onboard` rules):
  - `q4`: Q4_K_M > Q4_K_S > IQ4_XS > Q4_0
  - `5bit` (Q5_K_M), `6bit` (Q6_K), `q8` (Q8_0), `fp16` (F16/BF16)
- Put the preferred quant first and set `default:`.
- Add the mmproj (prefer Q8_0) as `role: mmproj`.
- Pick the adapter from the GGUF header:
  - `ggmlc.graph_spec` + decision recipe -> `ggmlc-laya`
  - llama.cpp architecture -> `ggmlc-custom-decider`
- Pin `size` + `sha256` from HF LFS metadata (`https://huggingface.co/api/models/<repo>/tree/main`, fields `lfs.size` and `lfs.oid`). No hash -> registry rejects it.
- Fill `info` (`context_length` and `max_options` are required by the tests) and `capabilities: {input, output}`.
- Model card lives at `models/readmes/<name>/<tag>.md`.
- Split GGUFs are not supported.

## `self check <model>`

- Downloads, starts the engine like `serve`, runs probes (`app.Probes`, shared with the `tests/models` end-to-end tests):
  - choice: obvious routing
  - choice: option order swap
  - noul: clearly true (> 0.6) / clearly false (< 0.4)
  - score: ordering (>= 1.5)
- Probes the model cannot answer (unsupported question type or too many options) are shown as `SKIP`.
- Any failed probe -> `Error [unsupported_model]`, exit 1.
- `self` reads the *published* registry (see 03), so a new entry can only be checked after the site deploy. Before that, the tests (`go test ./...`) validate the source file.

After `check` passes, `self benchmark <model>` measures the speed and writes a report to share (see 02).

## Acceptance criteria

### Requested integrations (2026-10-11)

The downloadable registry requires a compatible adapter, not just available
weights. These releases still need integration before registration:

| Model | Source | Required integration |
| --- | --- | --- |
| PPLX Embed v2 Late 0.6B | [perplexity-ai/pplx-embed-v2-late-0.6b](https://huggingface.co/perplexity-ai/pplx-embed-v2-late-0.6b) | The `multi-embedding` capability and token-matrix API with query/document modes are implemented. Runtime inference, learned 128-dimensional projection, bidirectional attention, markers, masking, normalization and pinned supported weights remain required. |
| PPLX Embed v2 Late 9B | [perplexity-ai/pplx-embed-v2-late-9b](https://huggingface.co/perplexity-ai/pplx-embed-v2-late-9b) | Uses the same implemented multi-vector contract; runtime and multimodal processing remain required. Both models share an embedding space; mean pooling would change the served task. |
| Pyramid-JiT 2.18B | [Linum-AI/pyramid-jit](https://huggingface.co/Linum-AI/pyramid-jit) | Pixel-space DiT inference and Qwen3.5-4B layer-feature conditioning. Its Apache-2.0 research weights are available, but the bundled sd-server does not implement this architecture. |
| Microsoft-Decision-1 | [Announcement](https://commandline.microsoft.com/microsoft-decision-1-model-foundry/) | Hosted Foundry/OpenRouter integration. The announcement provides hosted access, not downloadable local weights. |
| H2O-Lightning 4B | [Official GGUF runtime](https://huggingface.co/h2oai/h2o-lightning-4b-gguf) | Model-specific decision prompts, Qwen2/NFC normalization, label readout and calibration. Official BF16 and Q8 pass upstream text acceptance; the existing Decider prompt is not interchangeable. |
| THX-01 322M | [doofz/THX-01](https://huggingface.co/doofz/THX-01) | mmBERT encoder and trained decision head, MASK-position option scoring and calibration. The API and SELFIPC protocol support capability-gated number/excerpt/cite requests and typed extraction responses; the THX inference runtime and weight format integration remain required. A Laya encoder entry alone cannot serve these weights. |

Qwen Image 2.1 Turbo is registered as `qwen-image-2.1-turbo:7b` with pinned
Q4/Q8 weights and the published eight-step sigma schedule. On 2026-10-11,
both variants were downloaded and verified through `self pull`, started on an
RTX 5090 with `--device cuda`, and returned visually correct red-apple
generations as valid 512x512 PNGs. Q4 also passed a red-to-green single-image
edit and empty-prompt rejection (HTTP 400). Q8 editing, multiple-reference
editing, transparency and CPU inference remain unverified. See its model
card for the precise smoke-test coverage.

The other requested integrations above have not been run through `self`:
their inference adapters are not implemented or registered. The additive
THX and PPLX API contracts do not provide model inference. Microsoft-Decision-1
has no local weight release identified in its published deployment docs.

PPLX v2 Late registration is deferred. The Hugging Face search on 2026-10-11
found ONNX and MLX conversions, but no matching GGUF distribution for either
requested size. PPLX v1 GGUFs are different checkpoints and are not substitutes.
The multi-embedding API extension remains available for future compatible adapters.

- Must: mmproj detected, quants bucketed correctly (`internal/onboard` tests).
- Must: missing hash never passes the parser.
- Must: `self check` passes for `kev:0.5b`, `decider-vision:2b`, `decider:0.8b`, `decider:4b`, and `laya:english` (verified on CPU).
- Open: `self check` fails the `noul: clearly true` probe for `laya:multilingual` (noul=0.011) and `laya:typed-decisions` (noul=0.547); see 15.
