# self

`self` is a simple Dockerized wrapper around
[llama.cpp](https://github.com/ggml-org/llama.cpp),
[stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp), and
[ggmlc](https://github.com/monatis/ggmlc). It makes models easy to run and
manage through a typed HTTP API, downloading them directly from
[Hugging Face](https://huggingface.co/).

It supports:

- Decision making
- Audio processing
- Text processing
- Embedding generation
- Vision input
- Image generation
- Image editing

[Model registry](https://k0in.github.io/self/registry/)

The goal is get new models up and running quickly with minimal configuration, using established open-source tools.

This is mostly optimized for my personal use and development workflow and hardware (rtx 5090).

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

The `/models` volume keeps downloaded models between container runs.

## Configuration

The container entrypoint accepts `serve` options after the image name:

| Option | Default | Purpose |
| :--- | :--- | :--- |
| `serve <model>` | `kev:0.5b` | Model reference to download and serve; append `@quant` to select a quant |
| `--quant <name>` | Model default | Quantization, such as `q4`, `q8`, or `fp16` |
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
  ghcr.io/<owner>/<repo>:latest serve decider-vision:2b@q4
```

## Models

Models are downloaded directly from Hugging Face on first use, you can find the available models in the [Model registry](https://k0in.github.io/self/registry/).

To see how fast a model runs on your machine, run `self benchmark kev:0.5b`. It
writes a report you can share as a pull request, see [benchmarks/](benchmarks/README.md).

## HTTP API

See the full [OpenAI-style API reference](docs/api/index.md) for endpoint examples,
request and response fields, image inputs, Python and JavaScript clients, and
error responses. It has one page per modality: [decision](docs/api/decision.md),
[audio](docs/api/audio.md), and [text](docs/api/text.md),
[embeddings](docs/api/embeddings.md) and [speech to text](docs/api/stt.md).

