# self

Run a local AI model server with a registry-driven workflow.

[View the project on GitHub](https://github.com/k0in/self)

## Quick start

Native installation is not supported. The development commands below are
Docker-backed shorthand: `just setup` means running the image's `setup`
command, and `just serve <model>` means running the image's `serve <model>`
command with the host model directory mounted.

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

The server fetches the registry from `https://k0in.github.io/self/models.yml` at runtime and keeps a copy in the models directory (`.registry.yml`). If it cannot reach that URL or the registry is invalid, it logs a warning and uses the cached copy, so models that are already downloaded keep working offline. The very first run needs network access: without a cached registry the command fails, and there is no bundled registry. See [Docker and model paths](/docker) for running fully offline.

## Choose a model

Browse the [model registry](/registry/) or run:

```bash
self ls-remote
self pull kev:0.5b
```

The registry pins every download by size and SHA-256. A changed upstream artifact is rejected until the registry is updated.

## API

The default server listens on `127.0.0.1:8080` and exposes `/health`, `/v1/model`, and `/v1/systemone`. Use `--host 0.0.0.0` in a container so the published port is reachable.
