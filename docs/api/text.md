---
pageClass: api-page
aside: false
---

<div class="api-row">
<div class="api-doc">

# Text generation API

`self` exposes the OpenAI-compatible chat completions API for registry models
served by the upstream `llama-server` runtime. The official OpenAI SDKs work
unchanged. `self` owns the public port; `llama-server` listens on a private Unix
domain socket managed by `self`, with private loopback TCP as the Windows fallback.

The current bundled llama.cpp release supports text chat and image input for
the registered Qwen3.5 and Gemma 4 models. `self` downloads the model and its
projector, then starts the unmodified upstream `llama-server` with `--host`
set to the socket path in a `0700` temporary directory. The internal HTTP client
uses Unix `DialContext`, and the directory is removed after the child exits.
On Windows, `--host` is `127.0.0.1` and the engine uses a private TCP port.

```bash
self serve qwen3.5:9b
```

Other available chat models are `gemma4:e4b` and `gemma4:12b`.

</div>
<div class="api-example">

<div class="api-label">Endpoint</div>

```text
POST /v1/chat/completions
```

<div class="api-label">Request</div>

```json
{
  "model": "qwen3.5:9b",
  "messages": [
    {"role": "user", "content": "Explain embeddings in one sentence."}
  ],
  "max_tokens": 128,
  "stream": true
}
```

For a vision-capable model, use OpenAI image content. The image URL can be an
HTTPS URL or a base64 data URL:

```json
{
  "model": "gemma4:e4b",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "Describe this image in one word."},
        {"type": "image_url", "image_url": {"url": "https://example.com/image.jpg"}}
      ]
    }
  ],
  "max_tokens": 64,
  "temperature": 0
}
```

`qwen3.5:9b` thinks by default. Its reasoning is returned separately as
`reasoning_content`; use `"reasoning_effort": "none"` to request a direct
answer. Gemma 4 registry entries default to direct answers; use a non-`none`
reasoning effort to enable thinking.

The response uses standard `chat.completion` JSON for non-streaming requests
and `text/event-stream` chunks followed by `data: [DONE]` for streaming.
Reasoning-capable models may include `reasoning_content` alongside `content`.

The official OpenAI Python SDK works unchanged:

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="unused")
answer = client.chat.completions.create(
  model="qwen3.5:9b",
  messages=[{"role": "user", "content": "Say hello in one word."}],
  reasoning_effort="none",
  max_tokens=16,
)
print(answer.choices[0].message.content)
```

</div>
</div>
