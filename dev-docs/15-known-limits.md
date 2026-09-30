# 15 · Known limits and open points

## Limits

- Decision models only.
- Decider engine: max 10 options per choice question, 2-10 levels per score question, 1 image, plain layout only (no `"layout": "chat"`).
  Temperature defaults to 1.0; per-model values from `decider_config.json` are not read (use the `temperature` setting).
- Laya: option limit comes from the GGUF (16 by default), text only.
- No multi-part (split) GGUFs.
- One model per server process.
- No auto restart after engine crash. A request without an answer after 2 minutes kills the engine.
- sha256 checked on download only, not on every start (`self benchmark` does check it, and records it).
- `self benchmark` times the engine one request at a time, in-process: no HTTP, no queueing, no
  concurrency. It detects NVIDIA GPUs only (use `--gpu` to name another one) and judges GPU use
  from NVIDIA memory only, so a Metal or Vulkan run on other hardware is filed as a CPU run
  unless `--gpu` is given.
- The pre-filled GitHub link of `self benchmark` relies on the `value` parameter of GitHub's new-file
  page, which is not officially documented. The upload page link is the fallback, and reports too
  long for a link only get that one.
- Nothing aggregates `benchmarks/*.json` yet (no comparison table or docs page).
- Engines do not report the device they run on (`Device auto`), so the GPU memory the load adds is
  the only sign of GPU use.
- `self` always loads the published registry (`https://k0in.github.io/self/models.yml`). There is no flag or
  environment variable to use a local registry file, so a new entry cannot be run with `self` (or `just test-model`)
  before the Pages deploy.
- Offline start needs a registry cache from one earlier online run (no bundled registry). The cache can be outdated.
- `--runtime-dir` must be an absolute path: the engine starts in its own directory.
- Engine build tested on Linux x86_64 only.
- Container images: the `cuda-13` image bundles the CPU build of the Decider engine, and all images use the Vulkan build of Laya.
- The docs build (`docs/generate-registry.mjs`) does not list `max_images` as a reserved model key; a registry model that sets it would be rendered with a bogus quant.

## Open points

- `laya:multilingual` (q8) and `laya:typed-decisions` (q4) fail the `noul: clearly true` probe of `self check` and `just test-models`
  (noul=0.011 and 0.547, want > 0.6); `laya:multilingual` also fails the multi-question check. Either the model behaves differently
  from the probe's assumption or the model/adapter is wrong; not investigated.
- Visual check of the documentation site.
- Model cards are generated stubs. Need real text.
- macOS / Windows engine builds.
- `internal/onboard` is not reachable from the CLI (no `self onboard` command).
- `docs/ONBOARDING.md` (user docs) still mentions `just check-model` and `just settings`, which no longer exist.
- Engine crash seen once during manual testing (server on :8080, cause unknown).
  - Likely the engine binary was replaced by a rebuild while running.
  - Check with `just serve kev:0.5b --verbose` if it happens again.
