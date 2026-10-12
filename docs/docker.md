# Docker and model paths

The image contains the server and engine binaries, but model files live in `/models`.

CUDA images check for an accessible NVIDIA GPU at startup and exit with an error
if none is found. Run them with `--gpus=all` on a host with an NVIDIA driver and
the NVIDIA Container Toolkit installed. CPU images do not require a GPU.

```bash
mkdir -p "$HOME/.ai-server/models"
docker run --rm \
  --gpus=all \
  --publish 8080:8080 \
  --volume "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest serve kev:0.5b
```

The image sets `AI_SERVER_MODELS=/models` and starts `kev:0.5b` on `0.0.0.0:8080`. To use another registry model:

```bash
docker run --gpus=all --rm -p 8080:8080 -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest serve kev:4b@q4 --host 0.0.0.0 --port 8080
```

For hosts without an NVIDIA GPU, use the `cpu` tag without `--gpus=all`:

```bash
docker run --rm -p 8080:8080 -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:cpu serve kev:0.5b
```

## Offline use

On every successful registry fetch the server stores the exact response as
`/models/.registry.yml` and writes per-model `metadata.json` files in
`/models`. If the registry cannot be reached or is invalid, the server logs a
warning and uses that cached copy. Once a model has been downloaded, it can be
started without network access by mounting the same directory and using the
model reference as usual:

```bash
docker run --gpus=all --rm --network none -p 8080:8080 \
  -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest serve kev:4b --host 0.0.0.0 --port 8080
```

The same applies to `self ls`, `self ls-remote` (which lists the cached registry), `self settings` and `self check` for downloaded models.
