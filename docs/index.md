# self

Run a local AI model server with a registry-driven workflow.

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

The binary embeds the registry at build time. The GitHub Pages site publishes the same registry for browsing, but the server does not fetch it at runtime.

## Choose a model

Browse the [model registry](/registry/) or run:

```bash
self list
self suggest
self pull kev:0.5b
```

The registry pins every download by size and SHA-256. A changed upstream artifact is rejected until the registry is updated.

## API

The default server listens on `127.0.0.1:8080` and exposes `/health`, `/v1/model`, and `/v1/systemone`. Use `--host 0.0.0.0` in a container so the published port is reachable.
