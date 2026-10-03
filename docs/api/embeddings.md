---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Embeddings API

Embedding models are not registered yet, so this endpoint is not available.
The intended implementation will use the upstream llama.cpp `llama-server`
runtime and follow the standard OpenAI `/v1/embeddings` request and response
shapes. There is no embedding model type or route in the current build.

</div>
<div class="api-example">

<div class="api-label">Reserved endpoint</div>

```text
POST /v1/embeddings
```

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
