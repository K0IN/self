# 11 · Onboarding new models

## Flow

```
write registry entry (size + sha256 from HF LFS metadata)  ->  add readme + description  ->  models/registry.yml  ->  go test ./...  ->  publish (12)  ->  self check <model>
```

The entry is written by hand (see `docs/ONBOARDING.md` for the user-facing
guide). `internal/onboard` can analyze a Hugging Face repo (quant buckets,
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

- Must: mmproj detected, quants bucketed correctly (`internal/onboard` tests).
- Must: missing hash never passes the parser.
- Must: `self check` passes for `kev:0.5b`, `decider-vision:2b`, `decider:0.8b`, `decider:4b`, and `laya:english` (verified on CPU).
- Open: `self check` fails the `noul: clearly true` probe for `laya:multilingual` (noul=0.011) and `laya:typed-decisions` (noul=0.547); see 15.
