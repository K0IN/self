# 15 · Known limits and open points

## Limits

- Audio requests run one at a time on the loaded model; parallel requests queue.
  Measured on CPU (`--device cpu`), engine ready in under 1 s after the files
  are cached: Qwen3-TTS q4 about 4 s per short sentence, Pocket TTS about
  0.7 s (French, 24 layers: about 1.8 s).
- Audio `seed`: the same seed reproduces the output of a fresh engine, but not
  of a later request in the same process (the audio projector keeps its own RNG
  state). `top_k: 1` (greedy) is fully reproducible.
- Audio output is WAV at 24 kHz, also when OpenAI's default `mp3` is implied by
  an omitted `response_format`. Explicit non-WAV formats return 422.
- Audio `instructions` and non-default `speed` are rejected because the
  engine does not implement those controls.
- OpenAI built-in voice names (`alloy`, ...) all map to the model's default voice
  (Qwen3-TTS: the model's own; Pocket TTS: the registry `voice` file).
- Pocket TTS without any reference voice produces almost no audio; that is why
  its registry entries pin Kyutai's `default_voice.wav`. Its GGUFs come from
  `EryriLabs/pocket-tts-GGUF` (converted with llama.cpp's own converter);
  Kyutai and ggml-org publish none. Welsh (community-trained) is not listed.
  Other Pocket TTS GGUFs (`idle-intelligence`, `cstr`, `Serveurperso`) are for
  other engines and do not load in llama.cpp.
- Decision and audio models cannot be served together; one model is loaded per
  server process.
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
- `self` loads the published registry (`https://k0in.github.io/self/models.yml`) unless `--registry` /
  `AI_SERVER_REGISTRY` names another URL or file. `just` and the dev container point it at
  `models/registry.yml`, so a new entry can be tested before the Pages deploy.
- Offline start needs a registry cache from one earlier online run (no bundled registry). The cache can be outdated.
- `--runtime-dir` must be an absolute path: the engine starts in its own directory.
- Engine build tested on Linux x86_64 only.
- Clef has no prompt cache: every request evaluates its whole prompt (state, images, schema). On a GPU
  that is a few hundred ms; on a CPU-only host a request with an image and ~800 tokens takes about 30 s.
  An engine bundle built for another CUDA major than the host also runs on the CPU (see 13).
- Image registry and mocked HTTP tests do not validate native inference.
  Generation, editing and CUDA 12/13 execution still need hardware smoke tests.

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

- Audio open points:
  - `ggmlc-audio` has been run on CPU only; the `cuda-12.8`, `cuda-13.4` and
    `vulkan` builds are not tested yet.
  - Sampling settings apply to the whole engine, not per request.
  - Pocket TTS: one default voice per language model (en `alba`, fr `estelle`, the
    others Kyutai's `default_voice.wav`); the named voices in `kyutai/tts-voices`
    work as `ref_audio` but are not mapped to OpenAI voice names. The
    language voices `giovanni`, `lola`, `juergen` and `rafael` are in the gated
    `kyutai/pocket-tts` repo (needs `HF_TOKEN`), so no registry entry pins them.
  - Non-WAV output, streaming, `instructions` and `speed` are not implemented.
