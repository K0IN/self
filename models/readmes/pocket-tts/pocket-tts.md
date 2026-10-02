# Pocket TTS

Kyutai's Pocket TTS: a small text-to-speech model that clones a voice from a
short recording and runs comfortably on a CPU. Each language is its own model
(`pocket-tts:en`, `:de`, `:es`, `:fr`, `:it`, `:pt`); the language is fixed by
the weights. French is the 24-layer checkpoint (about 300M parameters), the
others have about 80M.

Source: [kyutai-labs/pocket-tts](https://github.com/kyutai-labs/pocket-tts).
Model files: [kyutai/pocket-tts](https://huggingface.co/kyutai/pocket-tts).
GGUF artifacts: [EryriLabs/pocket-tts-GGUF](https://huggingface.co/EryriLabs/pocket-tts-GGUF),
converted with llama.cpp's own converter and loaded by the unmodified llama.cpp
libraries in the `ggmlc-audio` engine. License: CC-BY-4.0.

The model needs a reference voice: without one it produces almost no audio.
Each language model uses the first voice listed in Kyutai's
[generate command documentation](https://github.com/kyutai-labs/pocket-tts#the-generate-command)
as its bundled default:

| Language | Default voice | Reference |
| --- | --- | --- |
| English | `alba` | [alba](https://huggingface.co/kyutai/tts-voices/blob/main/alba-mackenna/casual.wav) |
| Italian | `giovanni` | [giovanni](https://huggingface.co/kyutai/pocket-tts/blob/add_lang_not_documented/common_voice_it_36520747-enhanced-v2.mp3) |
| Spanish | `lola` | [lola](https://huggingface.co/kyutai/pocket-tts/blob/add_lang_not_documented/common_voice_es_19762977-enhanced-v2.mp3) |
| German | `juergen` | [juergen](https://huggingface.co/kyutai/pocket-tts/blob/add_lang_not_documented/de-DE-juergen.mp3) |
| Portuguese | `rafael` | [rafael](https://huggingface.co/kyutai/pocket-tts/blob/add_lang_not_documented/g-Vi8PgmSY0-enhanced-v2.wav) |
| French | `estelle` | [estelle](https://huggingface.co/kyutai/tts-voices/blob/main/unmute-prod-website/developpeuse-3.wav) |

When a request sends no `voice`, the server uses the default for the selected
language model. OpenAI voice names (`alloy`, ...) also select that model default;
they do not select a different named speaker. For example, an English request
uses `alba` automatically:

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

The same default voice is bundled with the English, German, Spanish, French,
Italian, and Portuguese models. To use another speaker, send a 5-20 second WAV
or MP3 reference recording with `ref_audio`; Pocket TTS clones that voice for
the request.

Clone another voice by sending a WAV/MP3 recording as `voice.audio` (base64) or
as the multipart file `ref_audio` on `POST /v1/audio/speech`. Use only voices
you own or have permission to use. Output is WAV at 24 kHz.

Measured on CPU (`pocket-tts:en`): engine ready in about 0.1 s, about 0.8 s for
a short sentence.

Spanish: do not start the input with `¡`, it can stop generation early.
