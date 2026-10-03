---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Text generation API

Text models expose an OpenAI-compatible chat completions endpoint. The server
runs one selected model per process. Vision-capable text models also accept
image content parts.

Health, model metadata and the error format are shared by all modalities; see
the [API overview](/api/).

</div>
<div class="api-example">

<div class="api-label">Endpoint</div>

```text
POST /v1/chat/completions
```

<div class="api-label">Models and quick start</div>

[Browse text models](/registry/?type=text)

```bash
self serve qwen3.5:9b
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

## `POST /v1/chat/completions`

Requests must use `Content-Type: application/json`. Unknown top-level fields
are rejected. `messages` is required and must not be empty.

#### Request body

<ApiField name="model" type="string" optional>

Accepted and ignored: a server runs the model selected at startup.

</ApiField>

<ApiField name="messages" type="array" required>

Messages in conversation order. Each item has `role` and `content`.
`role` is `system`, `developer`, `user`, or `assistant`; `developer` is treated
as `system`. Tool and function roles are unsupported.

`content` is a string or an array of `text` parts. Vision models also accept
user-only `image_url` parts with an HTTPS URL or image data URL.

</ApiField>

<ApiField name="max_tokens" type="integer" optional>

Maximum generated tokens. Must be at least `1`. `max_completion_tokens` takes
precedence when both are present.

</ApiField>

<ApiField name="max_completion_tokens" type="integer" optional>

Alias for `max_tokens`; must be at least `1`.

</ApiField>

<ApiField name="temperature" type="number" optional>

Sampling temperature from `0` through `2`.

</ApiField>

<ApiField name="top_p" type="number" optional>

Nucleus sampling value from `0` through `1`.

</ApiField>

<ApiField name="n" type="integer" optional>

Only `1` is supported.

</ApiField>

<ApiField name="stream" type="boolean" optional>

Defaults to `false`. With `true`, the response is server-sent events ending in
`data: [DONE]`.

</ApiField>

<ApiField name="stream_options" type="object" optional>

Only valid with `stream: true`. `include_usage: true` adds a final usage-only
chunk. `include_obfuscation` is accepted but has no effect.

</ApiField>

<ApiField name="stop" type="string | array of strings" optional>

One stop sequence or up to four sequences. Empty sequences are ignored.

</ApiField>

<ApiField name="presence_penalty" type="number" optional>

Sampling penalty from `-2` through `2`.

</ApiField>

<ApiField name="frequency_penalty" type="number" optional>

Sampling penalty from `-2` through `2`.

</ApiField>

<ApiField name="seed" type="integer" optional>

Seed passed to the runtime when supported by the loaded engine.

</ApiField>

<ApiField name="response_format" type="object" optional>

`type` is `text`, `json_object`, or `json_schema`. For `json_schema`, provide
`json_schema.schema` as a JSON schema object. `name`, `description`, and
`strict` are accepted inside `json_schema`.

</ApiField>

<ApiField name="reasoning_effort" type="string" optional>

`none` disables reasoning. `minimal`, `low`, `medium`, `high`, and `xhigh`
enable it.

</ApiField>

<ApiField name="chat_template_kwargs" type="object" optional>

Only `enable_thinking: true|false` is supported.

</ApiField>

<ApiField name="user, store, metadata, service_tier, prompt_cache_key, prompt_cache_retention, safety_identifier, parallel_tool_calls" type="any" optional>

Accepted for client compatibility and ignored. Tool calling, logprobs, audio,
prediction, web search, and non-text modalities return
`422 unsupported_capability` when non-empty. `tool_choice: "auto"` is accepted
but does not enable tools.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

::: code-group

```bash [curl]
curl http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "text-model",
    "messages": [
      {"role": "system", "content": "Answer briefly."},
      {"role": "user", "content": "Explain embeddings in one sentence."}
    ],
    "max_tokens": 64,
    "reasoning_effort": "none"
  }'
```

```python [Python]
import requests

response = requests.post(
    "http://localhost:8080/v1/chat/completions",
    json={
        "model": "text-model",
        "messages": [
            {"role": "user", "content": "Explain embeddings in one sentence."}
        ],
        "max_tokens": 64,
        "reasoning_effort": "none",
    },
    timeout=60,
)
response.raise_for_status()
print(response.json())
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/chat/completions', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({
    model: 'text-model',
    messages: [{ role: 'user', content: 'Explain embeddings in one sentence.' }],
    max_tokens: 64,
    reasoning_effort: 'none'
  })
})

if (!response.ok) throw new Error(await response.text())
console.log(await response.json())
```

:::

<div class="api-label">Example response</div>

```json
{
  "id": "chatcmpl-7f3d...",
  "object": "chat.completion",
  "created": 1760000000,
  "model": "text-model",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "Embeddings are numeric vectors that represent meaning."},
    "finish_reason": "stop",
    "logprobs": null
  }],
  "usage": {"prompt_tokens": 18, "completion_tokens": 10, "total_tokens": 28}
}
```

</div>
</div>

<div class="api-row">
<div class="api-doc">

#### Response body

<ApiField name="id" type="string">

Unique completion ID beginning with `chatcmpl-`.

</ApiField>

<ApiField name="object" type="string">

Always `chat.completion` for a non-streaming response.

</ApiField>

<ApiField name="created" type="integer">

Unix timestamp in seconds.

</ApiField>

<ApiField name="model" type="string">

ID of the loaded model.

</ApiField>

<ApiField name="choices" type="array">

One choice with `index: 0`, `message`, `finish_reason` (`stop` or `length`),
and `logprobs: null`.

</ApiField>

<ApiField name="choices[0].message" type="object">

`role` is `assistant`, `content` is generated text, and reasoning-capable
models may include separate `reasoning_content`.

</ApiField>

<ApiField name="usage" type="object">

`prompt_tokens`, `completion_tokens`, and `total_tokens` are integer token
counts. `total_tokens` is their sum.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Streaming request</div>

::: code-group

```bash [curl]
curl -N http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"Count to three."}],"stream":true,"stream_options":{"include_usage":true}}'
```

```python [Python]
import requests

with requests.post(
    "http://localhost:8080/v1/chat/completions",
    json={"messages": [{"role": "user", "content": "Count to three."}], "stream": True},
    stream=True,
    timeout=60,
) as response:
    response.raise_for_status()
    for line in response.iter_lines(decode_unicode=True):
        if line:
            print(line)
```

```js [JavaScript]
const response = await fetch('http://localhost:8080/v1/chat/completions', {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({
    messages: [{ role: 'user', content: 'Count to three.' }],
    stream: true
  })
})

if (!response.ok) throw new Error(await response.text())
console.log(await response.text())
```

:::

Each event is a `chat.completion.chunk`. Deltas contain `content` or
`reasoning_content`; the final chunk contains `finish_reason`, followed by
`data: [DONE]`. `include_usage` adds a final usage-only chunk.

</div>
</div>

<div class="api-row">
<div class="api-doc">

## Vision input

Vision models accept an image in a user message as an HTTPS URL or a supported
data URL. Check `/v1/model` for `capabilities.input` and `max_images`.

#### Image content part

<ApiField name="type" type="string" required>

Must be `image_url`.

</ApiField>

<ApiField name="image_url.url" type="string" required>

HTTPS URL or a JPEG, PNG, or WebP data URL.

</ApiField>

</div>
<div class="api-example">

<div class="api-label">Example request</div>

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "vision-model",
    "messages": [{"role": "user", "content": [
      {"type": "text", "text": "What color is the car?"},
      {"type": "image_url", "image_url": {"url": "https://example.com/car.jpg"}}
    ]}],
    "max_tokens": 32,
    "temperature": 0
  }'
```

<div class="api-label">Response</div>

```json
{
  "object": "chat.completion",
  "model": "vision-model",
  "choices": [{"index": 0, "message": {"role": "assistant", "content": "Red."}, "finish_reason": "stop", "logprobs": null}],
  "usage": {"prompt_tokens": 120, "completion_tokens": 2, "total_tokens": 122}
}
```

</div>
</div>

## Text errors

Besides the [shared errors](/api/#errors), text requests return `400
invalid_request` for malformed fields, `413 request_too_large` for bodies over
32 MiB, `422 unsupported_capability` for unsupported features, `429 queue_full`,
and runtime `503` or `504` errors. A streaming error after the first event is
written in-band as an `error` object.
