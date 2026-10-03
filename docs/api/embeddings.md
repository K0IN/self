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
route accepts one string or a non-empty array of strings.

#### Request body

<ApiField name="input" type="string | array of strings" required>

Text to embed, either one string or a list of strings.

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

## `POST /similarity`

Compute cosine similarity between `input` and every sentence in `ref`. Scores
are in the range `[-1, 1]` and have the same order as the supplied `ref` array.

#### Request body

<ApiField name="input" type="string" required>

The sentence to compare against.

</ApiField>

<ApiField name="ref" type="array of strings" required>

One or more sentences to compare with `input`.

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
identical vector direction, `0` means orthogonal vectors, and `-1` means
opposite direction. `usage.latency_ms` is the total time spent handling the
request, including all embedding calls.
Missing or empty fields, empty reference strings, unknown fields, mismatched
embedding dimensions, and zero-magnitude vectors return an error.

</div>
</div>
