---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Speech to text API

::: warning Not available yet
`self` cannot serve speech recognition (STT) models yet: there is no `stt`
model type in the registry and no transcription endpoint on the server. For
the opposite direction, text to speech, see the [audio API](/api/audio).
:::

When it lands, it will follow OpenAI's transcription and translation API, so
the official OpenAI SDKs work unchanged.

Until then, a request to the planned paths on any running server returns
`400 invalid_request` (no route).

</div>
<div class="api-example">

<div class="api-label">Planned endpoints</div>

```text
POST /v1/audio/transcriptions
POST /v1/audio/translations
```

<div class="api-label">Response today</div>

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
