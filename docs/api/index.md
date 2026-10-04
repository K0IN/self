---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# API reference

`self` exposes a small, OpenAI-style JSON API. The server is model-specific: start one model, then send requests to that model through the same base URL. The endpoints depend on the model's modality:

| Modality | Endpoints | Status |
| --- | --- | --- |
| [Decision](/api/decision) | `POST /v1/systemone`, `/v1/decide` | Available |
| [Audio (text to speech)](/api/audio) | `POST /v1/audio/speech`, `/v1/audio/voice`, `/v1/audio/voices` | Available, OpenAI-compatible |
| [Text generation](/api/text) | `POST /v1/chat/completions` | Available, OpenAI-compatible |
| [Embeddings](/api/embeddings) | `POST /v1/embeddings`, `POST /similarity` | llama.cpp embedding models |
| [Image generation and editing](/api/images) | `POST /v1/images/generations`, `POST /v1/images/edits` | OpenAI-compatible |
| [Speech to text](/api/stt) | `POST /v1/audio/transcriptions` | Reserved, not implemented |

This page covers what every model shares: health, model metadata and errors.

No API key is required by the server. Put it behind your own network boundary or reverse proxy when exposing it beyond localhost.

</div>
<div class="api-example">

<div class="api-label">Base URL</div>

```text
http://localhost:8080
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Health

### `GET /health`

Returns runner readiness. The HTTP status is `200` when ready and `503` otherwise.

#### Response body

<ApiField name="status" type="string">

`ok` while the model is ready, `unavailable` during shutdown or after an engine failure.

</ApiField>

<ApiField name="model" type="string">

ID of the loaded model.

</ApiField>

<ApiField name="runner" type="string">

Runner state, for example `ready`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/health
```

<div class="api-label">Response</div>

```json
{
  "status": "ok",
  "model": "kev:0.5b",
  "runner": "ready"
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Model metadata

### `GET /v1/model`

Returns the loaded model and effective settings.

#### Response body

<ApiField name="id" type="string">

Registry ID of the loaded model.

</ApiField>

<ApiField name="object" type="string">

Always `model`.

</ApiField>

<ApiField name="type" type="string">

Model type: `decision` or `audio`.

</ApiField>

<ApiField name="quant" type="string">

Quantization the model was loaded with.

</ApiField>

<ApiField name="capabilities" type="object">

What the model accepts and produces. The shape depends on the modality:

- Decision: `input.text`, `input.vision`, `input.multi_image` and `input.max_images` describe accepted inputs; `output.choice`, `output.score` and `output.noul` say which question types can be answered, and `output.max_options` is the largest number of options per question.
- Audio: `{"input": ["text"], "output": ["audio"]}`.

</ApiField>

<ApiField name="info" type="object" optional>

Model details from the registry, such as `family`, `parameters`, `context_length` and `license`.

</ApiField>

<ApiField name="settings" type="object" optional>

Effective engine settings, such as `threads`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/v1/model
```

<div class="api-label">Response</div>

```json
{
  "id": "kev:0.5b",
  "object": "model",
  "type": "decision",
  "quant": "q4",
  "capabilities": {
    "input": {"text": true, "vision": false, "multi_image": false, "max_images": 0},
    "output": {"choice": true, "score": true, "noul": true, "max_options": 16}
  },
  "info": {
    "family": "kev",
    "parameters": "0.5B",
    "context_length": 2048,
    "license": "apache-2.0"
  },
  "settings": {
    "threads": 4
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

### `GET /v1/models`

Returns an OpenAI-style model list. The server currently loads one model per process.

#### Response body

<ApiField name="object" type="string">

Always `list`.

</ApiField>

<ApiField name="data" type="array">

Model objects with `id`, `object`, `type` and `quant`, as in `GET /v1/model`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/v1/models
```

<div class="api-label">Response</div>

```json
{
  "object": "list",
  "data": [
    {
      "id": "kev:0.5b",
      "object": "model",
      "type": "decision",
      "quant": "q4"
    }
  ]
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Errors

Errors use a stable envelope with a machine-readable `type` and a human-readable `message`. These apply to every modality; the [decision](/api/decision#decision-errors) and [audio](/api/audio#audio-errors) pages list their own additional cases.

| Status | Error types | Meaning |
| --- | --- | --- |
| `400` | `invalid_request` | Invalid JSON, unknown fields, wrong content type, or an unknown route. |
| `405` | `invalid_request` | Wrong HTTP method for the route. |
| `413` | `image_too_large`, `request_too_large` | Request body exceeds 32 MiB. |
| `422` | `unsupported_capability` | The loaded model cannot answer the requested question, image input, audio format, stream, speed, or instruction request. |
| `429` | `queue_full` | The request queue is full. |
| `502` | `image_fetch_failed` | A remote image could not be fetched or decoded. |
| `503` | runtime errors | The model engine is unavailable, crashed, or shutting down. |
| `504` | `timeout` | The model did not answer before the request deadline. |

</div>
<div class="api-example">

<div class="api-label">Error response</div>

```json
{
  "error": {
    "type": "invalid_request",
    "message": "state is required"
  }
}
```

</div>
</div>
