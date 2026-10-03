---
pageClass: api-page
aside: false
---
<div class="api-row">
<div class="api-doc">
# Embeddings API
Embedding models use the `llama-server` adapter and expose an OpenAI-compatible
`/v1/embeddings` route.

</div>
<div class="api-example">

<div class="api-label">Endpoint</div>

```text
POST /v1/embeddings
```

<div class="api-label">Models and quick start</div>

[Browse embedding models](/registry/?type=embedding)

Register an embedding model with `type: embedding` and `adapter: llama-server`,
then start it with `self serve MODEL`.

The same loaded embedding model also exposes `POST /similarity` for comparing
one input sentence with a list of reference sentences.

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /similarity`

Compute cosine similarity between `input` and every sentence in `ref`. Scores
are in the range `[-1, 1]` and have the same order as the supplied `ref` array.

#### Request body

<ApiField name="input" type="string" required>

The reference sentence to compare against.

</ApiField>

<ApiField name="ref" type="array of strings" required>

One or more sentences to compare with `input`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl -i http://localhost:8080/similarity \
  -H 'Content-Type: application/json' \
  -d '{"input":"A cat is sleeping.","ref":["A kitten is asleep.","The weather is sunny."]}'
```

<div class="api-label">Response</div>

```json
[0.91, 0.08]
```

The response is a JSON array of numbers. A score of `1` means identical vector
direction, `0` means orthogonal vectors, and `-1` means opposite direction.
Missing or empty fields, empty reference strings, unknown fields, mismatched
embedding dimensions, and zero-magnitude vectors return an error.

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/embeddings`

The route accepts one string or a non-empty array of strings. Requests are
forwarded to llama.cpp's OpenAI-compatible `/v1/embeddings` endpoint.

#### Request body

<ApiField name="input" type="string | array of strings" required>

Text to embed, either one string or a list of strings.

</ApiField>

<ApiField name="model" type="string" optional>

Model ID is accepted for OpenAI client compatibility and ignored because a
server instance runs one loaded model.

</ApiField>

<ApiField name="encoding_format" type="string" optional>

Use `float` or omit the field. Other formats are not supported.

</ApiField>

<ApiField name="dimensions" type="integer" optional>

Accepted for OpenAI client compatibility, but dimensionality changes are not
supported by the loaded model and return `unsupported_capability`.

</ApiField>

<ApiField name="user" type="string" optional>

Optional end-user identifier.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl -i http://localhost:8080/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"embedding-model","input":"Represent this sentence."}'
```

```python [Python]
import requests

response = requests.post(
    "http://localhost:8080/v1/embeddings",
    json={"model": "embedding-model", "input": "Represent this sentence."},
    timeout=60,
)
print(response.status_code, response.json())
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/embeddings', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ model: 'embedding-model', input: 'Represent this sentence.' })
})
console.log(response.status, await response.text())
```

:::

<div class="api-label">Error behavior</div>

Unsupported `encoding_format` values and `dimensions` requests return an
`unsupported_capability` error. Other malformed requests return
`invalid_request`.

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
batch request, they cover all supplied input strings.

</ApiField>

</div>
<div class="api-example">

```json
{
  "object": "list",
  "data": [{"object": "embedding", "embedding": [0.0123, -0.0456], "index": 0}],
  "model": "embedding-model",
  "usage": {"prompt_tokens": 4, "total_tokens": 4}
}
```

</div>
</div>
