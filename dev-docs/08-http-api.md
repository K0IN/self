# 08 · HTTP API

## Package layout

HTTP infrastructure is split by ownership:

- `internal/api/server.go` mounts one model-type handler and owns shared
  middleware, health, model metadata, and error responses.
- `internal/api/decision/` owns `/v1/systemone` and `/v1/decide`.
- `internal/api/image/` is reserved for future image-model endpoints.

Adding another model type should add its handlers under `internal/api/<type>/`
and mount them through the shared `api.Mode` interface. Existing routes remain
unchanged.

## Endpoints

| Method | Path | Does |
| :--- | :--- | :--- |
| GET | `/health` | `{"status":"ok","model":…,"runner":"ready"}` |
| GET | `/v1/model` | Loaded model, capabilities, `info`, effective `settings` |
| GET | `/v1/models` | List (one entry today) |
| POST | `/v1/systemone` | Answer questions |
| POST | `/v1/decide` | Alias of `/v1/systemone` |

## Request

```json
{
  "state": "I was charged twice.",
  "images": ["https://…/1.webp"],
  "questions": {
    "refund": {"type": "noul", "instructions": "Refund needed?"},
    "dept": {"type": "choice", "instructions": "Which team?", "criteria": {"billing": "Payments", "tech": "Bugs"}},
    "urgency": {"type": "score", "instructions": "How urgent?", "criteria": ["low", "mid", "high"]}
  }
}
```

- `state`: string or object.
- `criteria`: list or `{key: description}`.
- `images`: URL string, data URI, or `{url, name, description}`.
- `model` optional. If set, must match the loaded model.

## Response

- `choice`: `choice`, `probabilities`, `confidence`.
- `score`: `score` (expected level index), `probabilities`, `confidence`.
- `noul`: `noul`, `confidence`.
- `usage`: `input_tokens`, `images`, `latency_ms`, `queue_ms`.

## Errors

`{"error":{"type":"queue_full","message":"…"}}`

| type | HTTP |
| :--- | :--- |
| `invalid_request`, `unsupported_image` | 400 |
| `model_not_found`, `quant_not_found` | 404 |
| `image_too_large` | 413 |
| `unsupported_capability` | 422 |
| `queue_full` | 429 |
| `image_fetch_failed` | 502 |
| `runtime_crashed`, `runtime_not_found`, `runtime_start_failed`, `shutting_down` | 503 |
| `timeout` | 504 |
| `internal_error` | 500 |

## Acceptance criteria

- Must: unknown question type / missing state -> 400.
- Must: images on a text-only model -> 422.
- Must: more options than `max_options` -> 422.
- Must: wrong `model` -> 404.
- Must: full queue -> 429.
- Must: every error uses the shape above (`internal/api/decision` tests with a fake adapter).
- Manual: curl examples in README work for every model.
