# Kyutai/Pocket TTS Italian 80M

- **License:** [CC-BY-4.0](https://creativecommons.org/licenses/by/4.0/) (verified in the original repository's public metadata)
- **Release date:** Not documented for this checkpoint in accessible upstream metadata; repository creation is not a model release date.
- **Modalities:** Input: text, optional reference audio; Output: audio
- **Original model:** [kyutai/pocket-tts](https://huggingface.co/kyutai/pocket-tts)

## Description

Pocket TTS is Kyutai's lightweight speech-generation model designed for efficient
CPU inference. It turns text into speech and can reproduce a speaker's voice
from a reference recording. This checkpoint generates Italian speech.
Description paraphrased from the [upstream README](https://github.com/kyutai-labs/pocket-tts).

## Run

```bash
self serve pocket-tts-it:100m
```

Here is a link to the API docs: [Audio API](/api/audio).

### Example Request

With the server running, open another terminal and send a request:

```bash
curl http://localhost:8080/v1/audio/speech \
        -H 'Content-Type: application/json' \
        -d '{"input":"Ciao, questo modello si chiama Pocket TTS."}' \
        -o output.wav
```

The response is saved as `output.wav`. Without request reference audio, the
adapter uses Kyutai's bundled `unmute-prod-website/default_voice.wav`.

## Engine Parameters

Served by the persistent `ggmlc-audio` engine through the `ggmlc-audio` adapter.
These are startup settings, not per-request JSON fields; the registry sets no
overrides, so the engine defaults apply.

| Parameter | Default | What it controls |
| --- | --- | --- |
| `context_size` | `4096` | Tokens shared by reference audio, text and generated frames; accepts 512 to 262144. |
| `threads` | Half the hardware concurrency (at least `1`) | CPU workers; accepts 1 to 1024. |
| `gpu_layers` | `-1` | GPU-offloaded layers; `-1` means all, `0` means CPU; accepts -1 to 10000. CPU device selection forces zero offload. |
| `temperature` | `0.8` | Token-sampler temperature; accepts 0.01 to 10. This is not a voice style or emotion control. |
| `top_p` | `0.95` | Nucleus sampling; accepts 0 to 1. |
| `top_k` | `40` | Top-k sampling; accepts 1 to 1000 (`1` selects greedy token sampling). |
| `frames` | `512` | Maximum generated audio frames per request; accepts 1 to 100000. |
| `seed` | Random | RNG seed; accepts 0 to 4294967295. Reproducibility depends on the pipeline and device. |

Override startup settings with `--set <parameter>=<value>`.

## Quantizations and Files

Default variant: `bf16`. This is the only registered variant; sizes and SHA-256
hashes are pinned in the model registry.

### bf16 (default)

| File | Role | Size | SHA-256 | Origin |
| --- | --- | --- | --- | --- |
| `italian/pocket-tts-italian.gguf` | model | 152.0 MiB (159391488 bytes) | `3e1b54accfefc85952c782aa3aedd5396cf5fcfb979ac1ab3f66e6c39e11b722` | [EryriLabs/pocket-tts-GGUF](https://huggingface.co/EryriLabs/pocket-tts-GGUF/blob/main/italian/pocket-tts-italian.gguf) |
| `italian/mmproj-pocket-tts-italian.gguf` | mmproj | 57.1 MiB (59858080 bytes) | `46f0a614c23c2fac6bd3dcf93202b227333b2fd67096abbfb4fb3d4ec78f4434` | [EryriLabs/pocket-tts-GGUF](https://huggingface.co/EryriLabs/pocket-tts-GGUF/blob/main/italian/mmproj-pocket-tts-italian.gguf) |
| `unmute-prod-website/default_voice.wav` | voice | 0.5 MiB (480044 bytes) | `27157be84054d6ce8af7a1ebca827c73d1d8fc38842f979d3fae207c92b1a9ae` | [kyutai/tts-voices](https://huggingface.co/kyutai/tts-voices/blob/main/unmute-prod-website/default_voice.wav) |

Total download: 209.6 MiB (219729612 bytes).

## Notes

- The serve ID retains `100m`, but the registry reports this checkpoint as 80M parameters. Language is fixed by the weights; a reference voice does not change it.
- Pocket TTS needs reference audio; the bundled file supplies it when none is sent. OpenAI voice names such as `alloy` select that same default, not different speakers. The upstream Italian voice `giovanni` is not bundled.
- To clone another voice, send WAV or MP3 as multipart `ref_audio` or base64 `voice.audio` on each request. Use only recordings you have permission to use; observe upstream's consent and prohibited-use terms.
- Output is WAV at 24 kHz. Upstream Python streaming and unlimited-text claims do not describe this adapter's bounded, non-streaming API; see the Audio API for supported request fields and limits.
- The bundled default recording is CC0 under the [voice repository's licensing notes](https://huggingface.co/kyutai/tts-voices/blob/main/README.md), separate from the model license.
- Origin links use `main` because these registry entries pin hashes, not repository revisions. Download size is not the total runtime memory requirement.