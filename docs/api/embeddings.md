---
pageClass: api-page
aside: false
---
<div class="api-row">
<div class="api-doc">

# Embeddings API
Embedding models expose the official OpenAI-compatible embeddings endpoint.
The server runs one selected embedding model per process.

Health, model metadata and the error format are shared by all modalities; see
the [API overview](/api/).

</div>
<div class="api-example">

<div class="api-label">Endpoint</div>

```text
POST /v1/embeddings
```

<div class="api-label">Models and quick start</div>

[Browse embedding models](/registry/?type=embedding)

```bash
self serve nomic-embed-text-v1.5:137m
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/embeddings`

This is the OpenAI-compatible embeddings endpoint. Requests must use
`Content-Type: application/json`; unknown top-level fields are rejected. The
route accepts one string or a non-empty batch of strings and content objects.
Unknown fields are rejected at every level. Content objects follow llama.cpp's
multimodal embedding extension; they are not part of the standard OpenAI text API.

#### Request body

<ApiField name="input" type="string | array of strings or content objects" required>

Text to embed, or a batch of `{content: [...]}` objects. Each content part is
`{type: "text", text: "..."}`, `{type: "image_url", image_url: {url: "data:image/png;base64,..."}}`,
or `{type: "input_audio", input_audio: {data: "...", format: "wav"}}`.
Audio data accepts raw base64 or an `audio/wav` base64 data URL. Images must be
PNG or JPEG data URLs or remote HTTPS URLs, using the same `image_url` content
part as the chat API. Media requires the corresponding loaded-model capability.

Media is limited to 10 MiB decoded per part; the JSON body limit is 32 MiB.
Images are limited to 16 megapixels and 8192 pixels per side. Audio must be
PCM16 WAV, mono or stereo, 8-96 kHz. Local file URLs are rejected.
Remote images use the same guarded fetcher as decision and chat models: HTTPS
and public destinations only by default, a 15-second fetch timeout, and at most
three redirects. Use `--allow-http-images` to permit plain HTTP, and
`--allow-private-images` to permit private or loopback destinations. These flags
weaken the default network restrictions and should only be enabled for trusted
clients. Audio remains inline-only.
Empty text, empty content, malformed media and mismatched MIME types return 400.
Unsupported content types or model modalities return 422 before any inference.

</ApiField>

<ApiField name="model" type="string" optional>

Accepted and ignored: a server runs the model selected at startup.

</ApiField>

<ApiField name="encoding_format" type="string" optional>

Use `float` or omit the field. Other formats are not supported.

</ApiField>

<ApiField name="dimensions" type="integer" optional>

Dimensionality changes are not supported by the loaded model and return
`422 unsupported_capability`.

</ApiField>

<ApiField name="user" type="string" optional>

Optional end-user identifier. It is accepted and ignored.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"nomic-embed-text-v1.5:137m","input":"Represent this sentence."}'
```

```python [Python]
import requests

response = requests.post(
    "http://localhost:8080/v1/embeddings",
    json={"model": "nomic-embed-text-v1.5:137m", "input": "Represent this sentence."},
    timeout=60,
)
response.raise_for_status()
print(response.json())
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/embeddings', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ model: 'nomic-embed-text-v1.5:137m', input: 'Represent this sentence.' })
})

if (!response.ok) throw new Error(await response.text())
console.log(await response.json())
```

:::

<div class="api-label">Response</div>

```json
{
  "object": "list",
  "data": [{"object": "embedding", "embedding": [0.0123, -0.0456], "index": 0}],
  "model": "nomic-embed-text-v1.5:137m",
  "usage": {"prompt_tokens": 4, "total_tokens": 4, "latency_ms": 12.4}
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Response body

<ApiField name="object" type="string">

`list`.

</ApiField>

<ApiField name="data" type="array">

Embedding objects with `object: "embedding"`, integer `index`, and a numeric
`embedding` array.

</ApiField>

<ApiField name="model" type="string">

Model used to create the embeddings.

</ApiField>

<ApiField name="usage" type="object">

Token counts returned by llama.cpp: `prompt_tokens` and `total_tokens`. For a
batch request, they cover all supplied input strings. `latency_ms` is the total
time spent handling the request, including embedding inference.

</ApiField>

</div>
<div class="api-example">

Embedding vectors are returned as floating-point arrays. The vector dimension
depends on the loaded model.

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Error behavior

Malformed JSON, missing input, empty batches, wrong content types, and unknown
fields return `400 invalid_request`. Unsupported `encoding_format` values and
`dimensions` requests return `422 unsupported_capability`.

</div>
<div class="api-example">

```json
{
  "error": {
    "type": "unsupported_capability",
    "message": "dimensions are not supported by the loaded embedding model"
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Multimodal Examples

Remote image input uses the same image content part as chat completions:

```json
{
  "input": [{"content": [
    {"type": "image_url", "image_url": {"url": "https://example.com/picture.png"}}
  ]}]
}
```

Start the multimodal model (the pinned projector downloads automatically):

```bash
self serve embeddinggemma-2:740m
```

The following Node.js example sends a text scalar, a text/image object, and an
audio object in one batch. Replace the two filenames with your own media files.

```js
import { readFileSync } from 'node:fs'

const image = `data:image/png;base64,${readFileSync('picture.png').toString('base64')}`
const audio = readFileSync('recording.wav').toString('base64')
const input = [
  'A bird singing in a tree.',
  { content: [
    { type: 'text', text: 'A bird in a tree.' },
    { type: 'image_url', image_url: { url: image } }
  ] },
  { content: [{ type: 'input_audio', input_audio: { data: audio, format: 'wav' } }] }
]
const response = await fetch('http://localhost:8080/v1/embeddings', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ input })
})
if (!response.ok) throw new Error(await response.text())
const result = await response.json()
console.log(result.data.map(item => ({ index: item.index, dimensions: item.embedding.length })))
```

EmbeddingGemma 2 returns full 768-dimensional vectors. Text-only scalar and
string-batch requests remain compatible. `/v1/models` reports the actual declared
input capabilities (`text`, `vision`, `audio` for this model).

Multimodal similarity uses a single string or content object as `input` and a
non-empty array of strings or content objects as `ref`:

```js
const response = await fetch('http://localhost:8080/similarity', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    input: { content: [{ type: 'image_url', image_url: { url: image } }] },
    ref: ['A bird in a tree.', { content: [{ type: 'input_audio', input_audio: { data: audio, format: 'wav' } }] }]
  })
})
if (!response.ok) throw new Error(await response.text())
console.log((await response.json()).similarities)
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /similarity`

Compute normalized cosine similarity between `input` and every item in
`ref`. Scores are in the range `[0, 1]` and have the same order as the supplied
`ref` array.

#### Request body

<ApiField name="input" type="string | content object" required>

The text or multimodal content object to compare against.

</ApiField>

<ApiField name="ref" type="array of strings or content objects" required>

One or more text or multimodal items to compare with `input`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/similarity \
  -H 'Content-Type: application/json' \
  -d '{"input":"A cat is sleeping.","ref":["A kitten is asleep.","The weather is sunny."]}'
```

<div class="api-label">Response</div>

```json
{
  "similarities": [0.91, 0.08],
  "usage": {"prompt_tokens": 10, "total_tokens": 10, "latency_ms": 24.7}
}
```

`similarities` contains one score per reference string. A score of `1` means
identical vector direction, `0.5` means orthogonal vectors, and `0` means
opposite direction. Scores are normalized from cosine similarity with
`(cosine + 1) / 2`; they are measured between `input` and each `ref`
independently, so they do not imply a global similarity across all references
or add up to 1. `usage.latency_ms` is the total time spent handling the
request, including all embedding calls.
Missing or empty fields, empty reference strings, unknown fields, mismatched
embedding dimensions, and zero-magnitude vectors return an error.

</div>
</div>
