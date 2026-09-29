# 03 · Registry

## What

- `models/registry.yml`: public, human-edited model list.
- Embedded in the binary (`models/embed.go`). Override with `--registry FILE`.
- Model cards: `models/readmes/<name>/<tag>.md`.

## Entry format

```yaml
decider:2b-vision:
  description: "Decider 2B Vision: one-pass decisions about text and one image"
  readme: readmes/decider/2b-vision.md
  type: decision
  default: 4bit
  capabilities: [text, vision, choice, score, noul]
  4bit:
    adapter: ggmlc-custom-decider
    repo: mradermacher/decider-2b-vision-GGUF
    files:
      - {file: decider-2b-vision.Q4_K_M.gguf, size: 1274397056, sha256: b3e3…}
      - {file: decider-2b-vision.mmproj-Q8_0.gguf, size: 364664448, sha256: 79e6…, role: mmproj}
```

## Rules

- Id: `name:tag`. No `/ \ :` or spaces in parts.
- `type`: only `decision` for now.
- `capabilities`: `text | vision | multi-image | choice | score | noul`. `multi-image` implies `vision`.
- Every non-reserved key is a quant (`4bit`, `8bit`, `f16`, …).
- `default` required if more than one quant.
- Files: GGUF only, clean relative paths.
- Roles: `model` (exactly one) and `mmproj` (optional).
- `size` (> 0) and `sha256` (64 hex) required on every file.
- `description`: one line, required.
- `readme`: relative `.md` path, required. No inline text, no URLs, no `../`.
- `info`: optional metadata. `family`, `parameters`, `architecture`, `base_model`, `source`, `license`, `languages`, `context_length`, `max_options`, `homepage`.
  - `max_options` also caps requests (422 above it).
  - Shown in `/v1/model` and on the site. Prefilled by `self onboard`.
- `settings`: optional engine parameters. Model level and/or per quant.
  - Keys: snake_case. Values: number, bool, string.
  - Checked against the adapter schema at resolve time (see 06).

## Models today

| Id | Adapter |
| :--- | :--- |
| `kev:0.5b`, `kev:0.8b`, `kev:4b` | `ggmlc-laya` |
| `laya:english`, `laya:multilingual`, `laya:typed-decisions` | `ggmlc-laya` |
| `decider:0.8b`, `decider:4b`, `decider:2b-vision` | `ggmlc-custom-decider` |

## Acceptance criteria

- Must: invalid entries fail to parse with a clear message (`internal/registry` tests).
- Must: missing `size`, bad `sha256`, missing/inline/escaping `readme`, missing `description` are rejected.
- Must: every `readme` in the bundled registry exists and starts with `# ` (`models/registry_test.go`).
- Must: every bundled model has `info.context_length` + `info.max_options`, and valid settings (`TestBundledRegistrySettings`).
- Must: unknown model -> `model_not_found` with suggestions. Unknown quant -> `quant_not_found`.
- Must: adding a compatible GGUF needs only a registry entry + readme.
