# 08 · HTTP API

## Package layout

HTTP infrastructure is split by ownership:

- `internal/api/server.go` mounts one model-type handler and owns shared
  middleware, health, model metadata, and error responses.
- `internal/api/decision/` owns `/v1/systemone` and `/v1/decide`.
- `internal/api/image/` is reserved for future image-model endpoints.

Adding another model type should add its handlers under `internal/api/<type>/`
and pass one `api.Mount` function to the shared router. That function registers
the type-specific routes and returns its metadata. Existing routes remain
unchanged.

## Endpoints

| Method | Path | Does |
| :--- | :--- | :--- |
| GET | `/health` | `{"status":"ok","model":…,"runner":"ready"}`. Runner `crashed` or `stopping` -> 503 with `"status":"unavailable"` |
| GET | `/v1/model` | Loaded model: `id`, `object`, `type`, `quant`, `capabilities` (`input` / `output`), `info`, effective `settings` |
| GET | `/v1/models` | `{"object":"list","data":[<model>]}` (one entry today) |
| POST | `/v1/systemone` | Answer questions |
| POST | `/v1/decide` | Alias of `/v1/systemone` |

Unknown routes return 400 `invalid_request`; a wrong method returns 405
(`Allow: GET, POST`) with the same body shape.

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

- `Content-Type` must be `application/json` when sent. Body limit 32 MiB (413 `image_too_large`). Unknown fields are rejected (400).
- `state`: required; a string or any other JSON value except `null`.
- `questions`: an object keyed by question id (order is kept), 1-64 questions. Each needs `type` and non-empty `instructions`.
  - `choice`: `criteria` is a list of keys or `{key: description}` (description may be `null`).
  - `score`: `criteria` is a non-empty list of level descriptions.
  - `noul`: no `criteria`.
- `images`: URL string, data URI, or `{url, name, description}` (see 10).
- `model` optional. If set, must match the loaded model.

## Response

`{"model": …, "answers": {<id>: …}, "usage": …}`. Answers keep the question order.

- `choice`: `type`, `choice`, `probabilities` (per option, in option order), `confidence`.
- `score`: `type`, `score` (expected level index), `probabilities` (keyed by level index `"0"`, `"1"`, …), `confidence`.
- `noul`: `type`, `noul` (probability the statement holds), `confidence`.
- `usage`: `input_tokens`, `output_tokens`, `images` (when images were sent), `latency_ms` (engine), `queue_ms`.

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
| `internal_error` (and kinds without a mapping, e.g. `unsupported_model`, `download_failed`) | 500 |

`internal_error` messages are replaced by `Internal server error.`

## Acceptance criteria

- Must: unknown question type / missing state -> 400.
- Must: images on a text-only model -> 422.
- Must: more options than `max_options` -> 422.
- Must: wrong `model` -> 404.
- Must: full queue -> 429.
- Must: every error uses the shape above (`internal/api/decision` tests with a fake adapter).
- Must: the served API matches this file for every model (end-to-end tests `api: *`, see 14).
- Manual (verified with `kev:0.5b`): `GET /health`, `/v1/model`, `/v1/models`, `POST /v1/systemone`, unknown route (400), wrong method (405).
