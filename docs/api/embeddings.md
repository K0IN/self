---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Embeddings API

::: warning Not available yet
`self` cannot serve embedding models yet: there is no `embeddings` model type
in the registry and no embeddings endpoint on the server.
:::

When it lands, it will follow OpenAI's embeddings API, so the official OpenAI
SDKs work unchanged, the same way the [audio API](/api/audio) does.

Until then, a request to the planned path on any running server returns
`400 invalid_request` (no route).

</div>
<div class="api-example">

<div class="api-label">Planned endpoint</div>

```text
POST /v1/embeddings
```

<div class="api-label">Response today</div>

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
