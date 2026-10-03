---
pageClass: api-page
aside: false
---
<div class="api-row">
<div class="api-doc">
# Embeddings API
Embedding models are not registered in the current build. There is no embedding
model type or `/v1/embeddings` route. The fields below describe the intended
OpenAI-compatible shape, not a live endpoint.

</div>
<div class="api-example">

<div class="api-label">Reserved endpoint</div>

```text
POST /v1/embeddings
```

<div class="api-label">Models and quick start</div>

[Browse embedding models](/registry/?type=embedding)

No embedding model is available yet, so there is no working `self serve`
command.

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/embeddings`

The route is not registered today. Planned request fields are listed for future
implementation.

#### Request body

<ApiField name="input" type="string | array of strings" required>

Text to embed, either one string or a list of strings.

</ApiField>

<ApiField name="model" type="string" required>

Planned embedding model ID.

</ApiField>

<ApiField name="encoding_format" type="string" optional>

Planned output encoding, such as `float` or `base64`.

</ApiField>

<ApiField name="dimensions" type="integer" optional>

Optional requested output dimension.

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

<div class="api-label">Current response</div>

```json
{
  "error": {
    "type": "invalid_request",
    "message": "no route for POST /v1/embeddings"
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Planned response body

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

Planned token counts: `prompt_tokens` and `total_tokens`.

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
