---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Audio Generation

Audio models expose an OpenAI-compatible speech endpoint. The server runs one
selected audio model per process and returns WAV audio.

Health, model metadata and the error format are shared by all modalities; see
the [API overview](/api/).

</div>
<div class="api-example">

<div class="api-label">Endpoints</div>

```text
POST /v1/audio/speech
POST /v1/audio/voice
POST /v1/audio/voices
```

<div class="api-label">Models and quick start</div>

[Browse audio models](/registry/?type=audio)

```bash
self serve qwen3-tts:1.7b
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## How voices work

Audio synthesis uses the text in `input` and, optionally, a short reference
recording that conditions the speaker identity for that request. The API has
three different voice concepts:

- An OpenAI voice name such as `alloy` is only an alias for the loaded model's
  default voice. It does not select a named speaker.
- `voice.audio` or `ref_audio` is inline reference audio. It is sent with the
  request and is not stored as a server-side voice profile.
- A registry `role: voice` file is a model's bundled default reference audio.
  It is used only when the request does not include reference audio.

When a request is synthesized, the server chooses reference audio in this
order. The first three forms are request-scoped; the last is bundled with the
model:

1. Inline audio in `voice.audio`.
2. A JSON `ref_audio` value.
3. A multipart `ref_audio` upload.
4. The model's registry-provided default reference voice.
5. No reference audio, if the model has no default voice.

If more than one reference-audio form is supplied, `voice.audio` wins over
JSON `ref_audio`, and JSON `ref_audio` wins over multipart `ref_audio`.

</div>
<div class="api-example">

<div class="api-label">Default voice</div>

```bash
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input":"Hello from the model default voice."}' \
  -o default.wav
```

<div class="api-label">Named voice alias</div>

```json
{"input":"Hello!","voice":"alloy"}
```

The accepted aliases are `alloy`, `ash`, `ballad`, `cedar`, `coral`, `echo`,
`fable`, `marin`, `nova`, `onyx`, `sage`, `shimmer`, and `verse`. They all use
the same loaded model default; they do not select different speakers.

<div class="api-label">Reference voice</div>

```json
{
  "input":"Hello in the reference speaker's voice.",
  "voice":{"audio":"<base64 MP3 or WAV>","format":"mp3"}
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/audio/speech`

Requests use `application/json` or `multipart/form-data`. The generated audio
is returned as `audio/wav`. The alias voice names accepted by OpenAI clients
select the loaded model's default voice; reference audio selects a speaker.

#### Request body

<ApiField name="input" type="string" required>

Text to synthesize, up to 4096 characters.

</ApiField>

<ApiField name="model" type="string" optional>

Accepted and ignored: the server runs one model.

</ApiField>

<ApiField name="voice" type="string | object" optional>

An OpenAI voice name such as `alloy`, which selects the default voice, or an
object containing inline base64 reference audio:

```json
{"audio":"<base64 MP3 or WAV>","format":"mp3"}
```

The object accepts `audio` and optional `format`; voice IDs are not supported.
The base64 value may also be a base64 `data:` URL. Filesystem paths are never
read.

</ApiField>

<ApiField name="response_format" type="string" optional>

Only `wav` is supported. The default is `wav`; other formats return
`422 unsupported_capability`.

</ApiField>

<ApiField name="stream_format" type="string" optional>

`audio` is accepted. `sse` is not implemented and returns
`422 unsupported_capability`.

</ApiField>

<ApiField name="speed" type="number" optional>

Validated from `0.25` through `4.0`, but only the default value `1.0` is
supported by the current runtime.

</ApiField>

<ApiField name="instructions" type="string" optional>

Accepted for compatibility but not supported by the runtime; a non-empty value
returns `422 unsupported_capability`.

</ApiField>

<ApiField name="language" type="string" optional>

Language passed to models that support it, for example `en`, `de`, `fr`, or
`ja`.

</ApiField>

<ApiField name="ref_audio" type="file | string" optional>

Server extension for reference audio. In multipart requests, upload an MP3 or
WAV file. In JSON, send base64 or a base64 `data:` URL. Filesystem paths are
rejected. Use this when a client cannot construct a `voice` object.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"model":"audio-model","input":"Hello from the local speech model.","voice":"alloy","response_format":"wav"}' \
  -o speech.wav
```

```python [Python]
import requests

response = requests.post(
    "http://localhost:8080/v1/audio/speech",
    json={
        "model": "audio-model",
        "input": "Hello from the local speech model.",
        "voice": "alloy",
        "response_format": "wav",
    },
    timeout=60,
)
response.raise_for_status()
with open("speech.wav", "wb") as audio:
    audio.write(response.content)
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/audio/speech', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({
    model: 'audio-model',
    input: 'Hello from the local speech model.',
    voice: 'alloy',
    response_format: 'wav'
  })
})

if (!response.ok) throw new Error(await response.text())
const audio = Buffer.from(await response.arrayBuffer())
require('fs').writeFileSync('speech.wav', audio)
```

:::

<div class="api-label">Response</div>

The response body is the generated WAV file with `Content-Type: audio/wav` and
`Content-Disposition: attachment; filename=speech.wav`.

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Reference-audio cloning

Reference-audio cloning here means conditioning: the recording is sent with
the text request and the model generates new WAV audio with a similar speaker
identity. It is not a persistent voice profile or a server-side fine-tune.

Use a clean 5 to 20 second recording of one speaker with little background
noise. This is a recommendation for cloning quality, not a server-enforced
duration limit. The reference must be MP3 or WAV. The server validates the
audio bytes, rejects empty or unsupported files, and keeps the recording only
in memory.

</div>
<div class="api-example">

<div class="api-label">Multipart request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/audio/speech \
  -F input='Hello, this is my cloned voice.' \
  -F language=en \
  -F ref_audio=@my-voice.mp3 \
  -o cloned.wav
```

```python [Python]
import requests

with open("my-voice.mp3", "rb") as reference:
    response = requests.post(
        "http://localhost:8080/v1/audio/speech",
        files={"ref_audio": ("my-voice.mp3", reference, "audio/mpeg")},
        data={"input": "Hello, this is my cloned voice.", "language": "en"},
        timeout=60,
    )
response.raise_for_status()
with open("cloned.wav", "wb") as audio:
    audio.write(response.content)
```

```js [JavaScript]
const form = new FormData()
form.append('input', 'Hello, this is my cloned voice.')
form.append('language', 'en')
form.append('ref_audio', new Blob([require('fs').readFileSync('my-voice.mp3')], { type: 'audio/mpeg' }), 'my-voice.mp3')

const response = await fetch('http://localhost:8080/v1/audio/speech', { method: 'POST', body: form })
if (!response.ok) throw new Error(await response.text())
require('fs').writeFileSync('cloned.wav', Buffer.from(await response.arrayBuffer()))
```

:::

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/audio/voice`

Packages a recording as a reusable JSON `voice` object. The alias
`/v1/audio/voices` behaves identically. This endpoint does not register a voice
or store it on the server; it only base64-encodes the uploaded bytes so a
client can keep them and send them to `/v1/audio/speech` later.

#### Request body

The recording can be uploaded as multipart field `audio_sample` or `file`, sent
as a raw audio body, or sent as JSON with base64 `audio` and optional `format`.

<ApiField name="audio_sample | file" type="file" conditional>

Multipart upload containing an MP3 or WAV file.

</ApiField>

<ApiField name="audio" type="string" conditional>

JSON base64 audio or a base64 `data:` URL.

</ApiField>

<ApiField name="format" type="string" optional>

`mp3` or `wav` when the format cannot be detected from the bytes.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/audio/voice \
  -F audio_sample=@my-voice.mp3 \
  -o voice.json
```

```python [Python]
import requests

with open("my-voice.mp3", "rb") as audio:
    response = requests.post(
        "http://localhost:8080/v1/audio/voice",
        files={"audio_sample": ("my-voice.mp3", audio, "audio/mpeg")},
        timeout=60,
    )
response.raise_for_status()
voice = response.json()["voice"]
print(voice["format"])
```

```js [JavaScript]
const form = new FormData()
form.append('audio_sample', new Blob([require('fs').readFileSync('my-voice.mp3')], { type: 'audio/mpeg' }), 'my-voice.mp3')

const response = await fetch('http://localhost:8080/v1/audio/voice', { method: 'POST', body: form })
if (!response.ok) throw new Error(await response.text())
console.log(await response.json())
```

:::

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

#### Response body

<ApiField name="object" type="string">

Always `voice`.

</ApiField>

<ApiField name="voice.audio" type="string">

Base64-encoded MP3 or WAV recording.

</ApiField>

<ApiField name="voice.format" type="string">

Detected audio format: `mp3` or `wav`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Reuse the response</div>

```bash
jq '{input: "Reusing my saved voice.", voice: .voice}' voice.json |
  curl http://localhost:8080/v1/audio/speech \
    -H 'Content-Type: application/json' -d @- -o reused.wav
```

</div>
</div>

## Audio errors

Besides the [shared errors](/api/#errors), audio requests return `400
invalid_request` for empty input, unknown fields or invalid reference audio,
`413 request_too_large` for bodies over 32 MiB, and
`422 unsupported_capability` for unsupported formats, streaming, instructions,
or speed values.
