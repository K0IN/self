# self

Run a local AI model server with a registry-driven workflow.

[View the project on GitHub](https://github.com/k0in/self)

## Quick start

The preferred way to run `self` is through Docker.
The development commands below are Docker-backed shorthand: `self serve <model>` means running the image's `docker run --rm -p 8080:8080 ghcr.io/k0in/self:latest serve <model>`.

```bash
# Docker-backed development
docker run --rm \
  -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest setup

docker run --rm -p 8080:8080 \
  -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest serve kev:0.5b

# Docker, with downloaded models persisted on the host
docker run --rm -p 8080:8080 \
  -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest
```

Images are published to GHCR for Linux hosts:

| Tag | Runtime |
| :--- | :--- |
| `latest` | NVIDIA CUDA 13 |
| `cuda-12` | NVIDIA CUDA 12 |
| `cpu` | CPU-only |

## Choose a model

Browse the [model registry](/registry/) or run:

```bash
self ls-remote
self pull kev:0.5b
```

The registry pins every download by size and SHA-256.
