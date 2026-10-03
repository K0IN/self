---
pageClass: api-page
aside: false
---
<div class="api-row">
<div class="api-doc">
# Speech to text API
Speech recognition is not implemented in the current build. There is no `stt`
model type, transcription handler, or registry model. The routes below are
reserved and do not accept requests yet.

</div>
<div class="api-example">

<div class="api-label">Reserved endpoints</div>

```text
POST /v1/audio/transcriptions
POST /v1/audio/translations
```

<div class="api-label">Models and quick start</div>

[Browse STT models](/registry/?type=stt)

No STT model is available yet, so there is no working `self serve` command.

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/audio/transcriptions`

This endpoint is not registered in the current build. The fields below describe
the planned OpenAI-compatible request and may change before STT support lands.

#### Request body

<ApiField name="file" type="file" required>

Audio upload to transcribe.

</ApiField>

<ApiField name="model" type="string" required>

Planned transcription model ID.

</ApiField>

<ApiField name="language" type="string" optional>

Optional source language code.

</ApiField>

<ApiField name="prompt" type="string" optional>

Optional context to guide transcription.

</ApiField>

<ApiField name="response_format" type="string" optional>

Planned output format, such as JSON or text.

</ApiField>

<ApiField name="temperature" type="number" optional>

Planned decoding temperature.

</ApiField>

<ApiField name="timestamp_granularities" type="array" optional>

Planned timestamp detail options.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl -i http://localhost:8080/v1/audio/transcriptions \
  -F file=@speech.wav \
  -F model=stt-model
```

```python [Python]
import requests

with open("speech.wav", "rb") as audio:
    response = requests.post(
        "http://localhost:8080/v1/audio/transcriptions",
        files={"file": ("speech.wav", audio, "audio/wav")},
        data={"model": "stt-model"},
        timeout=60,
    )
print(response.status_code, response.json())
```

```js [JavaScript]
const form = new FormData()
form.append('model', 'stt-model')
form.append('file', new Blob([require('fs').readFileSync('speech.wav')], { type: 'audio/wav' }), 'speech.wav')

const response = await fetch('http://localhost:8080/v1/audio/transcriptions', { method: 'POST', body: form })
console.log(response.status, await response.text())
```

:::

<div class="api-label">Current response</div>

```json
{
  "error": {
    "type": "invalid_request",
    "message": "no route for POST /v1/audio/transcriptions"
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/audio/translations`

The planned request uses the same multipart fields as transcription and returns
an English translation. This endpoint is also not registered today.

</div>
<div class="api-example">

```bash
curl -i http://localhost:8080/v1/audio/translations \
  -F file=@speech.wav \
  -F model=stt-model
```

```json
{"error":{"type":"invalid_request","message":"no route for POST /v1/audio/translations"}}
```

</div>
</div>
