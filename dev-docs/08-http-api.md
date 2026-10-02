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
| POST | `/v1/audio/speech` | Synthesize text as WAV audio for an audio model |
| POST | `/v1/audio/voice` | Normalize an inline WAV/MP3 voice payload |
| POST | `/v1/audio/voices` | Alias for `/v1/audio/voice` |

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
- `model` optional and ignored (accepted so SDK clients work): a server runs one model.

### Audio speech

Audio models expose an OpenAI-compatible request shape with a WAV response:

```json
{
  "model": "qwen3-tts:1.7b",
  "input": "Welcome home.",
  "voice": {"audio": "<base64 MP3 or WAV>", "format": "mp3"},
  "language": "en",
  "response_format": "wav"
}
```

`voice` may be an OpenAI built-in voice name (`alloy`, `ash`, ... all select the
model's default voice) or an object with inline `audio` and `format`. `model` may
be anything (it is ignored), so the official OpenAI SDKs work unchanged (verified with
`openai-go` v1.12.0). The
server-specific `ref_audio` field accepts only base64 audio or a `data:` URL
in JSON; filesystem paths are rejected. The endpoint also accepts
`multipart/form-data`: every field is a form field and `ref_audio` is an
uploaded file (unknown form fields -> 400). Reference audio must be MP3 or WAV;
the format comes from the magic bytes (`RIFF…WAVE`, `ID3` or an MP3 frame sync),
so a declared `format` is only a fallback.
Multipart bodies are read in memory with `Request.MultipartReader` (never
`ParseMultipartForm`, which spills large files to temp files on disk).
`response_format` accepts OpenAI's `mp3`,
`opus`, `aac`, `flac`, `wav`, and `pcm` values at the request boundary, while
the `ggmlc-audio` engine can produce only `wav`; other formats return
422, and an omitted format yields WAV. `stream_format` accepts OpenAI's `audio` and `sse` values at the request
boundary; streaming currently returns 422. The request body is limited to 32
MiB (413 `request_too_large`) and input text to 4096 characters. `instructions` and speeds other than
`1` are accepted as standard fields but return 422 because the engine does not
implement those controls.

### Stateless voice payloads

Turn a recording into a reusable voice object without storing it. The body may
be a form upload (`audio_sample`, as in OpenAI's create-voice API, or `file`),
the raw file (`Content-Type: audio/*` or `application/octet-stream`), or JSON:

```json
POST /v1/audio/voice
{"audio":"<base64 MP3 or WAV>","format":"mp3"}
```

The response contains the recording as base64. Supply that payload on
each speech request:

```json
{"input":"Hello.","voice":{"audio":"<base64>","format":"mp3"},"response_format":"wav"}
```

No voice data is stored by the API or the engine: the recording travels as an
IPC attachment and is decoded in memory for that one request. llama.cpp's
Qwen3-TTS pipeline conditions on the reference audio itself and has no API to
export or import a speaker vector, so the reusable client-owned payload is the
stateless voice representation. There is also no speaker-name or
voice-description input (ggml-org publishes only the Qwen3-TTS Base GGUF), so
OpenAI voice names select the default voice; named and described voices need
Qwen3-TTS CustomVoice/VoiceDesign support in llama.cpp first.

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
- Must: a `model` in the request is ignored (any value is accepted).
- Must: full queue -> 429.
- Must: every error uses the shape above (`internal/api/decision` tests with a fake adapter).
- Must: the served API matches this file for every model (end-to-end tests `api: *`, see 14).
- Manual (verified with `kev:0.5b`): `GET /health`, `/v1/model`, `/v1/models`, `POST /v1/systemone`, unknown route (400), wrong method (405).
