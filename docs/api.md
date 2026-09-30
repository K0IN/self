# API reference

`self` exposes a small, OpenAI-style JSON API for typed decisions. The server is model-specific: start one model, then send requests to that model through the same base URL.

```text
http://localhost:8080
```

No API key is required by the server. Put it behind your own network boundary or reverse proxy when exposing it beyond localhost.

## Health

### `GET /health`

Returns runner readiness.

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "ok",
  "model": "kev:0.5b",
  "runner": "ready"
}
```

`status` is `ok` while the model is ready and `unavailable` during shutdown or after an engine failure. The HTTP status is `200` when ready and `503` otherwise.

## Model metadata

### `GET /v1/model`

Returns the loaded model and effective settings.

```bash
curl http://localhost:8080/v1/model
```

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

### `GET /v1/models`

Returns an OpenAI-style model list. The server currently loads one model per process.

```bash
curl http://localhost:8080/v1/models
```

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

## Decisions

### `POST /v1/systemone`

The canonical decision endpoint. The `/v1/decide` path is an equivalent alias.

Headers:

```http
Content-Type: application/json
```

Request fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `state` | JSON value | yes | Context for the decision. It can be a string, object, array, number, or boolean. |
| `questions` | object | yes | Named questions to answer. Each key becomes the answer key. |
| `model` | string | no | Must match the loaded model ID when provided. |
| `images` | array | no | Images supplied to vision-capable models. |

Question fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `type` | `choice`, `score`, `noul` | yes | Output type requested from the model. |
| `instructions` | string | yes | What the model should decide. |
| `criteria` | object or array | depends | Options for `choice`; ordered levels for `score`; omitted for `noul`. |

`choice` criteria use an object whose keys are stable output values and whose values describe those choices:

```json
{
  "billing": "Payments, refunds, and invoices",
  "technical": "Bugs and technical problems"
}
```

`score` criteria use an ordered array:

```json
["low", "medium", "high"]
```

`noul` is a calibrated yes/no question and does not need criteria.

### Text example

```bash
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

Response fields:

| Field | Type | Description |
| --- | --- | --- |
| `model` | string | Loaded model ID. |
| `answers` | object | One answer keyed by each question ID. |
| `usage` | object | Runtime usage and latency counters. |

Example response:

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

Answer fields vary by question type:

| Answer type | Fields |
| --- | --- |
| `choice` | `choice`, `probabilities`, `confidence` |
| `score` | `score`, `probabilities` |
| `noul` | `noul`, `confidence` |

`choice` is the selected criteria key. `score` is a zero-based index into the submitted criteria array. `noul` is a probability from `0` to `1`, where values closer to `1` mean yes/true. `confidence` and probabilities are model output confidence values.

### Vision example

Vision models accept an image as an HTTPS URL or a data URI. An image can also be an object with optional `name` and `description` fields.

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

Supported image inputs are HTTPS URLs and JPEG, PNG, or WebP data URIs. Plain HTTP image URLs and private/loopback image addresses are rejected unless the server is started with `--allow-http-images` or `--allow-private-images`.

### Python

```python
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

### JavaScript

```js
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

## Errors

Errors use a stable envelope:

```json
{
  "error": {
    "type": "invalid_request",
    "message": "state is required"
  }
}
```

Common statuses:

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

Unknown JSON fields are rejected. Requests must use `Content-Type: application/json`.
