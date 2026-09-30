# 11 · Onboarding new models

## Flow

```
just onboard <hf-repo>  ->  edit readme + description  ->  paste into registry.yml  ->  just check-model <id>
```

## `self onboard <repo> [--id name:tag] [--readme-dir models]`

- Lists the HF repo (`/api/models/<repo>/tree/main`).
- Groups GGUFs into quants:
  - `q4`: Q4_K_M > Q4_K_S > IQ4_XS > Q4_0
  - `q8` (Q8_0), `fp16` (F16/BF16)
- First found = default.
- Picks mmproj (prefers Q8_0) as `role: mmproj`.
- Reads GGUF header via HTTP range requests (a few MiB).
- Picks adapter:
  - `ggmlc.graph_spec` + decision recipe -> `ggmlc-laya`
  - llama.cpp architecture -> `ggmlc-custom-decider`
- Pins `size` + `sha256` from HF LFS metadata.
- Writes a readme skeleton to `models/readmes/<name>/<tag>.md` (kept if it exists).
- No LFS hash -> `sha256: TODO` + warning (registry rejects it).
- Skips split GGUFs with a warning.

## `self check <model>`

- Downloads, starts the engine, runs probes:
  - choice: obvious routing
  - choice: option order swap
  - noul: clearly true (> 0.6) / clearly false (< 0.4)
  - score: ordering (>= 1.5)

## Acceptance criteria

- Must: generated YAML parses as a valid registry (`internal/onboard` tests).
- Must: mmproj detected, quants bucketed correctly.
- Must: missing hash never passes the parser.
- Must: `self check` passes for `kev:0.5b` and `decider-vision:2b@q4` (verified).
- Manual: onboard works on Mapika/decider-4b-GGUF, mradermacher/decider-0.8b-GGUF, mys/kev-0.8b-GGUF, mradermacher/decider-2b-vision-GGUF.
