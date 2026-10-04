---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Decision API

Decision models answer typed questions about a state in one pass: `choice`,
`score` and `noul` (calibrated yes/no). Vision-capable decision models also
accept images. [Browse Decision models](/registry/?type=decision)

</div>
<div class="api-example">

<div class="api-label">Endpoints</div>

```text
POST /v1/systemone
POST /v1/decide
```

<div class="api-label">Models and quick start</div>

[Browse decision models](/registry/?type=decision)

```bash
self serve kev:0.5b
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/systemone`

The canonical decision endpoint. The `/v1/decide` path is an equivalent alias.

#### Request body

<ApiField name="state" type="JSON value" required>

Context for the decision. It can be a string, object, array, number, or boolean.

</ApiField>

<ApiField name="questions" type="object" required>

Named questions to answer. Each key becomes the answer key in the response.

</ApiField>

<ApiField name="model" type="string" optional>

Ignored: the server runs one model. Accepted so SDK clients work unchanged.

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

Required for `choice` and `score`: defines the available choices or rating levels. Omit it for `noul`.

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
      "urgency": {
        "type": "score",
        "instructions": "How urgently should this ticket be handled?",
        "criteria": [
          "Low: routine request with no immediate impact",
          "Medium: customer affected, but a workaround exists",
          "High: customer blocked or money at risk"
        ]
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

<div class="api-label">Example response</div>

```json
{
  "model": "kev:0.5b",
  "answers": {
    "department": {
      "type": "choice",
      "choice": "billing",
      "probabilities": { "billing": 0.94, "technical": 0.06 },
      "confidence": 0.94
    },
    "urgency": {
      "type": "score",
      "score": 1.8,
      "probabilities": { "0": 0.05, "1": 0.1, "2": 0.85 },
      "confidence": 0.85
    },
    "refund_required": {
      "type": "noul",
      "noul": 0.91,
      "confidence": 0.91
    }
  },
  "usage": {
    "input_tokens": 31,
    "output_tokens": 0,
    "images": 0,
    "latency_ms": 24.7
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Criteria

`criteria` tells the model which answers or rating levels are available. Its format depends on the question's `type`.

**`choice`: pick one named option.** Supply a non-empty object:

- Each **key** is a value your application can receive, such as `"billing"`.
- Each **value** describes when to choose that option, such as `"Payments, refunds, and invoices"`.
- The response's `choice` contains the selected key, not its description. `probabilities` uses the same keys.

For the example, `"choice": "billing"` means the ticket should go to billing. If no descriptions are needed, a string array such as `["billing", "technical"]` is also accepted.

**`score`: rate on an ordered scale.** Supply a non-empty array of strings, from lowest to highest. Each string describes a level; its position assigns its numeric value, starting at `0`.

For `["low", "medium", "high"]`, the levels are `0`, `1`, and `2`. The response's `score` is the probability-weighted average of these values, so it can be fractional: `1.8` means a rating near `"high"`, not an array index to look up directly. `probabilities` uses the index keys `"0"`, `"1"`, and `"2"`.

**`noul`: estimate whether something is true.** Ask a yes/no question in `instructions` and omit `criteria`; this type does not accept choices or levels. The response's `noul` is a probability from `0` to `1`: near `0` means no, near `1` means yes. For example, `"noul": 0.91` means an estimated 91% probability that a refund is required, not a boolean result.

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
| `score` | `score`, `probabilities`, `confidence` |
| `noul` | `noul`, `confidence` |

`choice` is the selected criteria key. `score` is the probability-weighted average of the zero-based level indexes in the submitted criteria array. `noul` is a probability from `0` to `1`, where values closer to `1` mean yes/true. `confidence` and probabilities are model output confidence values.

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
    "urgency": {
      "type": "score",
      "score": 1.8,
      "probabilities": { "0": 0.05, "1": 0.1, "2": 0.85 },
      "confidence": 0.85
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

## Vision input

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
        "url": "https://upload.wikimedia.org/wikipedia/commons/thumb/b/b7/Damaged_fragile_parcel_delivered_to_doorstep.jpg/1280px-Damaged_fragile_parcel_delivered_to_doorstep.jpg",
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

<div class="api-label">Example response</div>

```json
{
  "model": "decider-vision:2b",
  "answers": {
    "damaged": {
      "type": "noul",
      "noul": 0.87,
      "confidence": 0.87
    }
  },
  "usage": {
    "input_tokens": 118,
    "output_tokens": 0,
    "images": 1,
    "latency_ms": 642.1
  }
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Decision errors

Besides the [shared errors](/api/#errors), decision requests return:

| Status | Error types | Meaning |
| --- | --- | --- |
| `400` | `unsupported_image` | The image is not an HTTPS URL or a JPEG, PNG, or WebP data URI. |
| `413` | `image_too_large` | Request body exceeds 32 MiB. |
| `422` | `unsupported_capability` | More options than the model supports, or images on a text-only model. |
| `429` | `queue_full` | The request queue is full. |
| `502` | `image_fetch_failed` | A remote image could not be fetched or decoded. |

</div>
<div class="api-example">

<div class="api-label">Error response</div>

```json
{
  "error": {
    "type": "unsupported_capability",
    "message": "The loaded decision model does not support image input."
  }
}
```

</div>
</div>
