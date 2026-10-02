# 03 · Registry

## What

- `models/registry.yml`: public, human-edited model list. The header comment of
  the file documents every field.
- Published as `https://k0in.github.io/self/models.yml`: the docs build copies
  the file to `docs/public/models.yml` (see 12), so a change reaches `self`
  only after the Pages deploy on push to `main`.
- `self` fetches that URL at run time (`registry.Fetch`: 30 s timeout, 16 MiB limit).
  There is no bundled copy.
- `--registry URL|FILE` / `AI_SERVER_REGISTRY` picks another source. An http(s) URL behaves
  like the published one (cache + offline fallback below). A file is read as-is, never cached,
  and has no fallback, so it cannot replace the cached published registry. `just` and the dev
  container set it to `models/registry.yml`; `tests/models` passes the same source to `self`.
- Model cards: `models/readmes/<name>/<tag>.md`.

## Offline cache

- For URL sources, `app.LoadRegistry` fetches the document (`registry.Fetch`), then `models.SaveRegistry` writes the exact
  response to `<models-dir>/.registry.yml` (atomic rename) plus `<name>/<tag>/metadata.json` per model.
- Fetch error, non-2xx, or unparsable document -> `models.LoadRegistryCache` and a `slog` warning.
  Applies to every command that loads the registry (`serve`, `pull`, `ls`, `ls-remote`, `settings`, `check`).
- No cache and no registry -> error naming both failures. No bundled registry.
- A failed cache write (read-only models dir) is a warning, not an error.
- Downloaded files need no network: `Store.Installed` checks size only; `Ensure` downloads nothing when all files are present.

## Entry format

```yaml
version: 1
models:
  "decider-vision:2b":
    description: "Decider 2B Vision: one-pass decisions about text and one image"
    readme: readmes/decider-vision/2b.md
    type: decision
    default: q4
    capabilities:
      input: [text, vision]
      output: [choice, score, noul]
    info: {family: decider, parameters: 2B, context_length: 262144, max_options: 10}
    settings: {context_size: 8192, temperature: 1.0}
    q4:
      adapter: ggmlc-custom-decider
      repo: mradermacher/decider-2b-vision-GGUF
      files:
        - {file: decider-2b-vision.Q4_K_M.gguf, size: 1274397056, sha256: b3e3…}
        - {file: decider-2b-vision.mmproj-Q8_0.gguf, size: 364664448, sha256: 79e6…, role: mmproj}
```

## Rules

- Document: `version: 1` (anything else is rejected) and a `models:` mapping.
- Id: `name:tag`. No `/ \ :` or spaces in parts.
- CLI references may append `@quant`, for example `decider-vision:2b@q4`.
  The `@quant` portion is not part of the registry ID.
- `type`: `decision` or `audio`. Types and what each accepts (capabilities, file roles) are declared
  in `modelTypes` in `internal/registry/types.go`. A model of a type this build does not know is skipped
  without judging its other fields (`Registry.Skipped`), so a registry that gained a type still loads for
  older clients. `self ls-remote` lists skipped ids as needing a newer `self`. A missing `type` is an error.
- `capabilities`: a mapping with `input` (`text`, `vision`, `multi-image`) and
  `output` (`choice`, `score`, `noul`) lists. `multi-image` implies `vision`.
  Optional `max_images` (model level) sets the image limit of `multi-image`
  models. It defaults to 1 for vision models and is capped by what the engine
  reports.
- Reserved model keys: `type`, `default`, `capabilities`, `max_images`,
  `description`, `readme`, `info`, `settings`. Every other key is a quant
  (`q4`, `q8`, `fp16`, …). Quant names may not contain `/` or `\`.
- `default` required if more than one quant.
- Per quant: `adapter` (must be a known adapter, checked at resolve time),
  `repo` (`owner/name`), `files`, optional `settings`.
- Files: GGUF only, clean relative paths. Exceptions: the audio `voice` role is a
  `.wav` or `.mp3` file; the decision `head` role is a `.safetensors` file.
- Optional per-file `repo` (`owner/name`) when a file lives in another repo than
  the quant's (e.g. a default voice from `kyutai/tts-voices`, a head published by the model author).
- Optional per-file `revision` selects a Hugging Face branch, tag, or commit; it
  defaults to `main`.
- Roles are per type. Decision: `model` (exactly one), `mmproj` (optional) and
  `head` (optional trained decision head, used by `clef`).
  Audio: `model`, `mmproj` (required by `ggmlc-audio`) and `voice` (optional
  default reference voice, used when a request sends none). Each role at most
  once. The first file may omit `role` (it is the model); additional files must
  name a role.
- `size` (> 0) and `sha256` (64 hex) required on every file.
- `description`: one line, required.
- `readme`: relative `.md` path, required. No inline text, no URLs, no `../`.
- `info`: optional metadata. `family`, `parameters`, `architecture`, `base_model`, `source`, `license`, `languages`, `context_length`, `max_options`, `homepage` (https only).
  - `max_options` also caps requests (422 above it, see 08).
  - Shown in `/v1/model` and on the site.
- `settings`: optional engine parameters. Model level and/or per quant.
  - Keys: snake_case. Values: number, bool, string.
  - Checked against the adapter schema at resolve time (see 06).

## Models today

| Id | Adapter |
| :--- | :--- |
| `kev:0.5b`, `kev:0.8b`, `kev:4b` | `ggmlc-laya` |
| `laya:english`, `laya:multilingual`, `laya:typed-decisions` | `ggmlc-laya` |
| `decider:0.8b`, `decider:4b`, `decider-vision:2b` | `ggmlc-custom-decider` |
| `qwen3-tts:1.7b` | `ggmlc-audio` |
| `pocket-tts-en:100m`, `-de`, `-es`, `-fr`, `-it`, `-pt` (each `:100m`) | `ggmlc-audio` (with a default `voice` file) |

## Acceptance criteria

- Must: invalid entries fail to parse with a clear message (`internal/registry` tests).
- Must: missing `size`, bad `sha256`, missing/inline/escaping `readme`, missing `description` are rejected.
- Must: every `readme` of `models/registry.yml` exists and no bundled model is of an unknown type (`internal/registry` `TestBundledRegistry`; the docs build fails on a missing readme too).
- Must: a model of an unknown type is skipped, not fatal (`TestParseSkipsUnknownType`).
- Must: every source model has `default`, an adapter, and pinned files (`tests/models` `TestSupportedModels`).
- Must: every source model has `info.context_length` + `info.max_options`, and valid settings (`TestBundledRegistrySettings`).
- Must: unknown model -> `model_not_found` with suggestions. Unknown quant -> `quant_not_found`.
- Must: registry down, non-2xx, invalid or unreachable -> cached registry is used; no cache -> error (`TestLoadRegistryFallsBackToCache`).
- Must: unwritable cache dir does not fail a successful fetch (`TestLoadRegistryIgnoresUnwritableCache`).
- Must: adding a compatible GGUF needs only a registry entry + readme.

The tests above read the source file `models/registry.yml`; `self` itself uses
the published copy, which lags until the site is deployed.
