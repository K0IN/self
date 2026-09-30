# self

Run a local AI model server with a registry-driven workflow.

[View the project on GitHub](https://github.com/k0in/self)

## Quick start

```bash
# Native development
just setup
just serve kev:0.5b

# Docker, with downloaded models persisted on the host
docker run --rm -p 8080:8080 \
  -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest
```

The server fetches the registry from `https://k0in.github.io/self/models.yml` at runtime. If it cannot reach that URL or the registry is invalid, the command fails; it does not fall back to a bundled or local registry.

## Choose a model

Browse the [model registry](/self/registry/) or run:

```bash
self list
self suggest
self pull kev:0.5b
```

The registry pins every download by size and SHA-256. A changed upstream artifact is rejected until the registry is updated.

## API

The default server listens on `127.0.0.1:8080` and exposes `/health`, `/v1/model`, and `/v1/systemone`. Use `--host 0.0.0.0` in a container so the published port is reachable.
