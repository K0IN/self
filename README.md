# self

[View the source on GitHub](https://github.com/k0in/self)

`self` is a simple Dockerized wrapper around
[llama.cpp](https://github.com/ggml-org/llama.cpp),
[stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp), and
[ggmlc](https://github.com/monatis/ggmlc). It makes models easy to run and
manage through a typed HTTP API, downloading them directly from
[Hugging Face](https://huggingface.co/) instead of requiring a custom model
registry.

The goal is to be as ergonomic as Ollama while using a good open-source ggml
engine for each function, with text, image, audio, and video support in one
server.

## Run with Docker

Images are published to GHCR for Linux hosts:

| Tag | Runtime |
| :--- | :--- |
| `latest` | NVIDIA CUDA 13 |
| `cuda-12` | NVIDIA CUDA 12 |
| `cpu` | CPU-only |

CUDA 13:

```bash
docker run --rm --gpus all \
  -p 8080:8080 \
  -v self-models:/models \
  ghcr.io/<owner>/<repo>:latest
```

CPU-only:

```bash
docker run --rm \
  -p 8080:8080 \
  -v self-models:/models \
  ghcr.io/<owner>/<repo>:cpu
```

The default model is `kev:0.5b`. Select another model by changing the command:

```bash
docker run --rm --gpus all -p 8080:8080 -v self-models:/models \
  ghcr.io/<owner>/<repo>:latest serve kev:4b
```

The `/models` volume keeps downloaded models between container runs. The
server listens on `http://127.0.0.1:8080` from the host. Podman uses the same
image names and options.

Immutable image tags contain the commit, for example
`cuda-13-0123456789abcdef`.

## Configuration

The container entrypoint accepts `serve` options after the image name:

| Option | Default | Purpose |
| :--- | :--- | :--- |
| `serve <model>` | `kev:0.5b` | Model reference to download and serve; append `@quant` to select a quant |
| `--quant <name>` | Model default | Quantization, such as `4bit` or `8bit` |
| `--device <name>` | `auto` | `cpu`, `cuda`, `cuda:N`, `vulkan`, or `vulkan:N` |
| `--queue-size <n>` | `64` | Maximum queued requests |
| `--preprocess-concurrency <n>` | `8` | Concurrent image preprocessing jobs |
| `--verbose` | off | Show engine and request logs |
| `--allow-http-images` | off | Allow non-HTTPS image URLs |
| `--allow-private-images` | off | Allow private or loopback image addresses |

Environment variables can override container defaults:

| Variable | Purpose |
| :--- | :--- |
| `AI_SERVER_HOST` | Listen host |
| `AI_SERVER_PORT` | Listen port |
| `AI_SERVER_MODELS` | Model directory; keep this as `/models` when using the volume |
| `HF_TOKEN` | Token for gated Hugging Face repositories |
| `HF_ENDPOINT` | Hugging Face mirror endpoint |

For example:

```bash
docker run --rm --gpus all -p 9000:9000 -v self-models:/models \
  -e AI_SERVER_PORT=9000 \
  ghcr.io/<owner>/<repo>:latest serve decider-vision:2b@4bit --host 0.0.0.0
```

## Models

Models are downloaded directly from Hugging Face on first use. The following
models are currently supported:

| Model | Input | Outputs |
| :--- | :--- | :--- |
| `kev:0.5b`, `kev:0.8b`, `kev:4b` | Text | `choice`, `score`, `noul` |
| `laya:english`, `laya:multilingual`, `laya:typed-decisions` | Text | `choice`, `score`, `noul` |
| `decider:0.8b`, `decider:4b` | Text | `choice`, `score`, `noul` |
| `decider-vision:2b` | Text and one image | `choice`, `score`, `noul` |

Text-only models reject requests containing images. Model files remain in the
`/models` volume and are reused on subsequent runs.

The server caches the model registry and per-model `metadata.json` files in
the `/models` volume. After a model has been downloaded successfully, it can
be started offline from that cache. A model that has never been downloaded
still requires one online run to discover its metadata and files.

To run a previously cached model offline, mount the same `/models` volume and
start the model as usual:

```bash
docker run --rm --network none --gpus all \
  -p 8080:8080 \
  -v self-models:/models \
  ghcr.io/<owner>/<repo>:latest serve kev:4b
```

## HTTP API

### Health

```bash
curl -s http://127.0.0.1:8080/health
```

### Decision request

```bash
curl -s http://127.0.0.1:8080/v1/systemone \
  -H 'Content-Type: application/json' \
  -d '{
    "state": "I was charged twice.",
    "questions": {
      "department": {
        "type": "choice",
        "instructions": "Which department should handle this?",
        "criteria": {
          "billing": "Payments and refunds",
          "technical": "Technical problems"
        }
      },
      "refund": {
        "type": "noul",
        "instructions": "Does this likely require a refund?"
      }
    }
  }'
```

The response contains an answer for each question and request usage:

```json
{
  "model": "kev:0.5b",
  "answers": {
    "department": {"type": "choice", "choice": "billing"},
    "refund": {"type": "noul", "noul": 0.89, "confidence": 0.89}
  },
  "usage": {"input_tokens": 13, "output_tokens": 0, "latency_ms": 6.0}
}
```

Endpoints:

| Method | Path | Purpose |
| :--- | :--- | :--- |
| `GET` | `/health` | Readiness and runner status |
| `GET` | `/v1/model` | Loaded model and capabilities |
| `GET` | `/v1/models` | Loaded model list |
| `POST` | `/v1/systemone` | Answer typed decision questions |
| `POST` | `/v1/decide` | Alias for `/v1/systemone` |

Question types are `choice`, `score`, and `noul`. A `score` returns the
expected zero-based level index and probabilities; `noul` returns calibrated
yes/no probability. Images may be HTTPS URLs or JPEG, PNG, and WebP data URIs.

Common HTTP errors are `400` invalid requests, `404` model errors, `413`
oversized images, `422` unsupported capabilities, `429` queue full, `502`
image fetch failures, `503` runtime failures, and `504` timeouts.

## Scope

The current image implements the decision API and includes a vision-capable
model. Audio, video, image generation, and broader LLM APIs are planned. The
container is the supported distribution method.
