---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Embeddings API

Embedding models are not registered in the current build. There is no embedding model type or `/v1/embeddings` route, so requests cannot be served yet. This page records the reserved OpenAI-compatible shape without implying that it works today.

</div>
<div class="api-example">

<div class="api-label">Reserved endpoint</div>

```text
POST /v1/embeddings
Content-Type: application/json
```

<div class="api-label">curl request today</div>

```bash
curl -i http://localhost:8080/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"embedding-model","input":"Represent this sentence."}'
```

</div>
</div>

## Current response

The shared router returns HTTP `400` because the route is not registered:

```json
{"error":{"type":"invalid_request","message":"no route for POST /v1/embeddings"}}
```

## Planned request and response

The intended request will contain required `input` (a string or array of strings) and `model` (accepted for OpenAI compatibility). Optional `encoding_format`, `dimensions`, and `user` follow the OpenAI API but are not supported or validated by this build.

The planned response is an `object: "list"` with `data` entries containing `object: "embedding"`, an integer `index`, and a floating-point `embedding` array, plus `model` and `usage` (`prompt_tokens`, `total_tokens`).
