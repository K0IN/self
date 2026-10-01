---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Audio API (text to speech)

Audio models expose an OpenAI-compatible speech endpoint. Start an audio model,
such as `qwen3-tts:1.7b` or `pocket-tts:en`, before sending requests. The
endpoint returns WAV audio and does not require an API key.

The official OpenAI SDKs work unchanged: point their base URL at
`http://localhost:8080/v1`. OpenAI model names (`tts-1`, `tts-1-hd`,
`gpt-4o-mini-tts`) select the loaded model, and built-in voice names select its
default voice.

Health, model metadata and the error format are shared by all modalities; see
the [API overview](/api/).

The model is loaded once when the server starts and stays loaded, so a request
only pays for generation (on CPU, per short sentence: about 4 s for
`qwen3-tts:1.7b` q4, under 1 s for `pocket-tts`). Requests are processed one at
a time; concurrent requests wait in a queue.

</div>
<div class="api-example">

<div class="api-label">Endpoints</div>

```text
POST /v1/audio/speech
POST /v1/audio/voice
POST /v1/audio/voices
```

<div class="api-label">OpenAI SDK (Python)</div>

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="unused")
with client.audio.speech.with_streaming_response.create(
    model="tts-1",
    voice="alloy",
    input="Hello from the local speech model.",
    response_format="wav",
) as response:
    response.stream_to_file("speech.wav")
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Choosing a voice

The voice is set by **reference audio**, not by a name or a text description:

| You send | You get |
| --- | --- |
| No `voice` | The model's default voice (for Pocket TTS, Kyutai's default voice, downloaded with the model) |
| An OpenAI voice name (`alloy`, `ash`, `coral`, ...) | The same default voice; the name is accepted so OpenAI clients work, it does not select a speaker |
| A short recording of a speaker (MP3 or WAV) | That speaker's voice (voice cloning) |

A clean recording of 5 to 20 seconds of one person speaking works best.
The server never stores it: send it with every request.

Named speakers and voices described in text are not available yet. Qwen3-TTS
has them in separate model variants (CustomVoice and VoiceDesign), but
llama.cpp b11256 supports only the Base model, and its TTS engine accepts only
a language and a reference recording. Pocket TTS models speak one language
each (`pocket-tts:en`, `:de`, `:es`, `:fr`, `:it`, `:pt`); a recording in any
language works as the reference.

</div>
<div class="api-example">

<div class="api-label">Default voice</div>

```json
{"input": "Hello!"}
```

<div class="api-label">Cloned voice (upload a recording)</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -F input='Hello!' \
  -F ref_audio=@my-voice.mp3 \
  -o hello.wav
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Examples

Copy-paste examples against a server started with
`self serve qwen3-tts:1.7b`. Each one writes a WAV file you can play.

**Speak some text** with the default voice.

**OpenAI-style request**: the same body an OpenAI client sends.

**Another language**: set `language` (`en`, `de`, `fr`, `es`, `it`, `pt`,
`ru`, `ja`, `ko`, `zh`).

**Clone a voice in one call**: upload a recording as a file with
`multipart/form-data`. No base64 needed; all speech fields can be form fields.

**Clone a voice with JSON**: when your client can only send JSON, put the
recording in `voice.audio` as base64.

</div>
<div class="api-example">

<div class="api-label">Speak some text</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input": "Hello world, this is a local text to speech server."}' \
  -o hello.wav
```

<div class="api-label">OpenAI-style request</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"model": "tts-1", "voice": "alloy",
       "input": "The quick brown fox jumps over the lazy dog."}' \
  -o openai.wav
```

<div class="api-label">Another language</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input": "Guten Morgen! Wie geht es dir heute?", "language": "de"}' \
  -o german.wav
```

<div class="api-label">Clone a voice in one call</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -F input='Hello, this is my cloned voice.' \
  -F language=en \
  -F ref_audio=@my-voice.mp3 \
  -o cloned.wav
```

<div class="api-label">Clone a voice with JSON (bash)</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d "{\"input\": \"Cloned via JSON.\",
       \"voice\": {\"audio\": \"$(base64 -w0 my-voice.mp3)\"}}" \
  -o cloned.wav
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/audio/speech`

Requests use `Content-Type: application/json` or `multipart/form-data`. In a
form, every field below is a form field and `ref_audio` is an uploaded file. The
standard OpenAI fields are accepted:

<ApiField name="input" type="string" required>

Text to synthesize, up to 4096 characters.

</ApiField>

<ApiField name="model" type="string" optional>

The loaded model ID or an OpenAI TTS model name (`tts-1`, `tts-1-hd`,
`gpt-4o-mini-tts`), so the official OpenAI SDKs work unchanged.

</ApiField>

<ApiField name="voice" type="string | object" optional>

An OpenAI built-in voice name (`alloy`, `ash`, `coral`, ...), which selects the
model's default voice, or an object containing inline base64 reference audio for
voice cloning:

```json
{"audio":"<base64 MP3 or WAV>","format":"mp3"}
```

Voices are never stored by the server; send the reference audio with every
request.

</ApiField>

<ApiField name="response_format" type="string" optional>

OpenAI values `mp3`, `opus`, `aac`, `flac`, `wav`, and `pcm` are recognized at
the request boundary. The current llama.cpp runtime can produce only `wav`;
other formats return `422 unsupported_capability`. When omitted, the response
is WAV (OpenAI's default is MP3).

</ApiField>

<ApiField name="stream_format" type="audio | sse" optional>

Both OpenAI values are recognized. Streaming is not implemented yet and
`sse` returns `422 unsupported_capability`.

</ApiField>

<ApiField name="speed" type="number" optional>

The OpenAI range `0.25` to `4.0` is validated. The current runtime supports
only the default value `1.0`; other values return `422`.

</ApiField>

<ApiField name="instructions" type="string" optional>

Accepted for OpenAI request compatibility, but returns `422` because the
bundled llama.cpp TTS runtime does not currently expose instruction control.

</ApiField>

<ApiField name="language" type="string" optional>

Language passed to Qwen3-TTS, for example `en`, `de`, `fr`, or `ja`.

</ApiField>

<ApiField name="ref_audio" type="file | string" optional>

Server extension for the reference recording. In a form, upload the MP3 or WAV
file itself; in JSON, send base64 or a base64 `data:` URL. Filesystem paths are
rejected. The format is detected from the file.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen3-tts:1.7b",
    "input": "Hello from the local speech model.",
    "voice": {
      "audio": "<base64 MP3 or WAV>",
      "format": "mp3"
    },
    "language": "en",
    "response_format": "wav"
  }' \
  --output speech.wav
```

<div class="api-label">Response</div>

The response body is the generated WAV file with `Content-Type: audio/wav`.

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/audio/voice`

Turns a recording into a `voice` object you can reuse in JSON speech requests,
without base64-encoding it yourself. Nothing is stored: the response contains
the recording, so keep it and send it again with each request. The alias
`POST /v1/audio/voices` behaves identically.

Send the recording in one of three ways:

- a form upload, field `audio_sample` (as in OpenAI's create-voice API) or `file`;
- the raw file as the body, with `Content-Type: audio/mpeg`, `audio/wav` or
  `application/octet-stream`;
- JSON `{"audio": "<base64>"}`.

The file must be MP3 or WAV; the format is detected from its content.

</div>
<div class="api-example">

<div class="api-label">Upload a file</div>

```bash
curl http://localhost:8080/v1/audio/voice \
  -F audio_sample=@my-voice.mp3 \
  -o voice.json
```

<div class="api-label">Or send the file as the body</div>

```bash
curl http://localhost:8080/v1/audio/voice \
  -H 'Content-Type: audio/mpeg' \
  --data-binary @my-voice.mp3 \
  -o voice.json
```

<div class="api-label">Reuse it in a JSON request (jq)</div>

```bash
jq '{input: "Reusing my saved voice.", voice: .voice}' voice.json |
  curl http://localhost:8080/v1/audio/speech \
    -H 'Content-Type: application/json' -d @- -o reused.wav
```

<div class="api-label">Response</div>

```json
{
  "object": "voice",
  "voice": {
    "audio": "<base64 of your recording>",
    "format": "mp3"
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Audio errors

Besides the [shared errors](/api/#errors), audio requests return:

| Status | Error types | Meaning |
| --- | --- | --- |
| `400` | `invalid_request` | Empty or too long `input`, unknown voice name or form field, or reference audio that is not an MP3/WAV file (filesystem paths included). |
| `413` | `request_too_large` | Request body exceeds 32 MiB. |
| `422` | `unsupported_capability` | Non-WAV `response_format`, `stream_format: "sse"`, `instructions`, or `speed` other than `1`. |

</div>
<div class="api-example">

<div class="api-label">Error response</div>

```json
{
  "error": {
    "type": "unsupported_capability",
    "message": "response_format \"mp3\" is not supported by the loaded audio model"
  }
}
```

</div>
</div>
