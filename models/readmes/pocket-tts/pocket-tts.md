# Pocket TTS

Kyutai's Pocket TTS: a small text-to-speech model that clones a voice from a
short recording and runs comfortably on a CPU. Each language is its own model
(`pocket-tts-en:100m`, `pocket-tts-de:100m`, `pocket-tts-es:100m`,
`pocket-tts-fr:100m`, `pocket-tts-it:100m`, `pocket-tts-pt:100m`); the language
is fixed by the weights. French is the 24-layer checkpoint (about 300M
parameters), the others have about 80M.

Source: [kyutai-labs/pocket-tts](https://github.com/kyutai-labs/pocket-tts).
Model files: [kyutai/pocket-tts](https://huggingface.co/kyutai/pocket-tts).
GGUF artifacts: [EryriLabs/pocket-tts-GGUF](https://huggingface.co/EryriLabs/pocket-tts-GGUF),
converted with llama.cpp's own converter and loaded by the unmodified llama.cpp
libraries in the `ggmlc-audio` engine. License: CC-BY-4.0.

## Voices

Pocket TTS clones the speaker from a reference recording and needs one: without
it the model produces almost no audio. Every model therefore bundles a default
voice, which is used whenever a request sends no `voice`:

| Model | Default voice | Reference |
| --- | --- | --- |
| `pocket-tts-en:100m` | `alba` | [alba-mackenna/casual.wav](https://huggingface.co/kyutai/tts-voices/blob/main/alba-mackenna/casual.wav) |
| `pocket-tts-fr:100m` | `estelle` | [unmute-prod-website/developpeuse-3.wav](https://huggingface.co/kyutai/tts-voices/blob/main/unmute-prod-website/developpeuse-3.wav) |
| `pocket-tts-de:100m`, `-es`, `-it`, `-pt` | Kyutai's default voice | [unmute-prod-website/default_voice.wav](https://huggingface.co/kyutai/tts-voices/blob/main/unmute-prod-website/default_voice.wav) |

OpenAI voice names (`alloy`, ...) are accepted but select the same bundled
default; they do not select a different named speaker. An English request uses
`alba` automatically:

```bash
curl http://localhost:8080/v1/audio/speech \
	-H 'Content-Type: application/json' \
	-d '{"input":"Hello from Pocket TTS."}' \
	-o output.wav
```

For an OpenAI-compatible client, a built-in name such as `alloy` is accepted
and produces the same bundled default voice:

```bash
curl http://localhost:8080/v1/audio/speech \
	-H 'Content-Type: application/json' \
	-d '{"model":"tts-1","voice":"alloy","input":"Hello from Pocket TTS."}' \
	-o output.wav
```

### Use another voice

Send a 5-20 second WAV or MP3 recording of one speaker as `ref_audio`
(multipart file) or as `voice.audio` (base64) on `POST /v1/audio/speech`. The
recording can be in any language; the model keeps speaking its own. Nothing is
stored, so send it with every request.

Kyutai's [kyutai/tts-voices](https://huggingface.co/kyutai/tts-voices) has many
ready-made voices (the licence differs per folder, see its README):

```bash
curl -L -o merchant.wav \
  https://huggingface.co/kyutai/tts-voices/resolve/main/alba-mackenna/merchant.wav
curl http://localhost:8080/v1/audio/speech \
  -F input='Hello from Pocket TTS.' \
  -F ref_audio=@merchant.wav \
  -o output.wav
```

The other language voices Kyutai names (`giovanni`, `lola`, `juergen`,
`rafael`) are in the gated [kyutai/pocket-tts](https://huggingface.co/kyutai/pocket-tts)
repo, so they are not bundled; with access you can send them the same way.
Use only voices you own or have permission to use. Output is WAV at 24 kHz.

Measured on CPU (`pocket-tts-en:100m`): engine ready in about 0.1 s, about 0.8 s for
a short sentence.

Spanish: do not start the input with `¡`, it can stop generation early.
