# self

Run a local AI model server with a registry-driven workflow.

[View the project on GitHub](https://github.com/k0in/self)

## Quick start

The preferred way to run `self` is through Docker.
The development commands below are Docker-backed shorthand: `self serve <model>` means running the image's `docker run --rm --gpus=all -p 8080:8080 ghcr.io/k0in/self:latest serve <model>`.

```bash
docker run --rm -p 8080:8080 \
  --gpus=all \
  -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest serve kev:0.5b
```

Images are published to GHCR for Linux hosts:

| Tag | Runtime |
| :--- | :--- |
| `latest` | NVIDIA CUDA 13 |
| `cuda-12` | NVIDIA CUDA 12 |
| `cpu` | CPU-only |

CUDA images require an accessible NVIDIA GPU, a host NVIDIA driver, and the
NVIDIA Container Toolkit. They exit at startup if no GPU is found. On hosts
without a GPU, use the `cpu` tag and omit `--gpus=all`.

## Choose a model

Browse the [model registry](/registry/) or run:

```bash
self ls-remote
self pull kev:0.5b
```

The registry pins every download by size and SHA-256.
