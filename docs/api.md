---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# API reference

`self` exposes a small, OpenAI-style JSON API for typed decisions. The server is model-specific: start one model, then send requests to that model through the same base URL.

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

Model type, for example `decision`.

</ApiField>

<ApiField name="quant" type="string">

Quantization the model was loaded with.

</ApiField>

<ApiField name="capabilities" type="object">

What the model accepts and produces.

- `input.text`, `input.vision`, `input.multi_image` and `input.max_images` describe accepted inputs.
- `output.choice`, `output.score` and `output.noul` say which question types can be answered, and `output.max_options` is the largest number of options per question.

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

## Decisions

### `POST /v1/systemone`

The canonical decision endpoint. The `/v1/decide` path is an equivalent alias. Requests must use `Content-Type: application/json`, and unknown JSON fields are rejected.

#### Request body

<ApiField name="state" type="JSON value" required>

Context for the decision. It can be a string, object, array, number, or boolean.

</ApiField>

<ApiField name="questions" type="object" required>

Named questions to answer. Each key becomes the answer key in the response.

</ApiField>

<ApiField name="model" type="string" optional>

Must match the loaded model ID when provided.

</ApiField>

<ApiField name="images" type="array" optional>

Images supplied to vision-capable models. See [Vision input](#vision-input).

</ApiField>

#### Question object

<ApiField name="type" type="choice | score | noul" required>

Output type requested from the model.

</ApiField>

<ApiField name="instructions" type="string" required>

What the model should decide.

</ApiField>

<ApiField name="criteria" type="object | array" optional>

Options for `choice` and ordered levels for `score`. Omit it for `noul`.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/systemone \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "kev:0.5b",
    "state": {
      "message": "I was charged twice for the same order.",
      "customer_tier": "pro"
    },
    "questions": {
      "department": {
        "type": "choice",
        "instructions": "Which department should handle this?",
        "criteria": {
          "billing": "Payments and refunds",
          "technical": "Technical problems"
        }
      },
      "refund_required": {
        "type": "noul",
        "instructions": "Does this likely require a refund?"
      }
    }
  }'
```

```python [Python]
import requests

response = requests.post(
    "http://localhost:8080/v1/systemone",
    json={
        "state": "Classify this support ticket.",
        "questions": {
            "priority": {
                "type": "choice",
                "instructions": "What priority should this ticket receive?",
                "criteria": {
                    "low": "No immediate impact",
                    "high": "Production or customer impact",
                },
            }
        },
    },
    timeout=60,
)
response.raise_for_status()
print(response.json())
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/systemone', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({
    state: { message: 'The invoice total is wrong.' },
    questions: {
      billing: {
        type: 'choice',
        instructions: 'Is this a billing issue?',
        criteria: { yes: 'Billing issue', no: 'Not a billing issue' }
      }
    }
  })
})

if (!response.ok) throw new Error(await response.text())
console.log(await response.json())
```

:::

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Criteria

`choice` criteria use an object whose keys are stable output values and whose values describe those choices.

`score` criteria use an ordered array of levels.

`noul` is a calibrated yes/no question and does not need criteria.

</div>
<div class="api-example">

<div class="api-label">choice</div>

```json
{
  "billing": "Payments, refunds, and invoices",
  "technical": "Bugs and technical problems"
}
```

<div class="api-label">score</div>

```json
["low", "medium", "high"]
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Response body

<ApiField name="model" type="string">

ID of the loaded model.

</ApiField>

<ApiField name="answers" type="object">

One answer for each question, keyed by the question ID. The fields depend on the question type:

| Answer type | Fields |
| --- | --- |
| `choice` | `choice`, `probabilities`, `confidence` |
| `score` | `score`, `probabilities` |
| `noul` | `noul`, `confidence` |

`choice` is the selected criteria key. `score` is a zero-based index into the submitted criteria array. `noul` is a probability from `0` to `1`, where values closer to `1` mean yes/true. `confidence` and probabilities are model output confidence values.

</ApiField>

<ApiField name="usage" type="object">

Runtime usage and latency counters.

- `input_tokens` and `output_tokens` count the tokens processed and generated.
- `images` is the number of images processed.
- `latency_ms` is the time spent running the model.
- `queue_ms` is the time the request waited in the queue.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Response</div>

```json
{
  "model": "kev:0.5b",
  "answers": {
    "department": {
      "type": "choice",
      "choice": "billing",
      "probabilities": {
        "billing": 0.91,
        "technical": 0.09
      },
      "confidence": 0.91
    },
    "refund_required": {
      "type": "noul",
      "noul": 0.97,
      "confidence": 0.97
    }
  },
  "usage": {
    "input_tokens": 23,
    "output_tokens": 0,
    "images": 0,
    "latency_ms": 18.4
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

### Vision input

Vision models accept an image as an HTTPS URL or a data URI. An image can also be an object with optional `name` and `description` fields.

Supported image inputs are HTTPS URLs and JPEG, PNG, or WebP data URIs. Plain HTTP image URLs and private/loopback image addresses are rejected unless the server is started with `--allow-http-images` or `--allow-private-images`.

#### Image object

<ApiField name="url" type="string" required>

HTTPS URL or a JPEG, PNG, or WebP data URI.

</ApiField>

<ApiField name="name" type="string" optional>

Optional name for the image.

</ApiField>

<ApiField name="description" type="string" optional>

Optional description of the image.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/v1/systemone \
  -H 'Content-Type: application/json' \
  -d '{
    "state": "Inspect this image and decide whether the package is damaged.",
    "images": [
      {
        "url": "https://example.com/package.jpg",
        "name": "package",
        "description": "Front camera image of the delivered package"
      }
    ],
    "questions": {
      "damaged": {
        "type": "noul",
        "instructions": "Is the package visibly damaged?"
      }
    }
  }'
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Errors

Errors use a stable envelope with a machine-readable `type` and a human-readable `message`.

| Status | Error types | Meaning |
| --- | --- | --- |
| `400` | `invalid_request`, `unsupported_image` | Invalid JSON, fields, content type, or image input. |
| `404` | `model_not_found`, `quant_not_found` | Requested model does not match the loaded model or registry. |
| `413` | `image_too_large` | Request body exceeds 32 MiB. |
| `422` | `unsupported_capability` | The loaded model cannot answer the requested question or image input. |
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
