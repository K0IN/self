# Docker and model paths

The image contains the server and engine binaries, but model files live in `/models`. Mount a host directory there so downloads survive container replacement:

```bash
mkdir -p "$HOME/.ai-server/models"
docker run --rm \
  --publish 8080:8080 \
  --volume "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest
```

The image sets `AI_SERVER_MODELS=/models` and starts `kev:0.5b` on `0.0.0.0:8080`. To use another registry model:

```bash
docker run --rm -p 8080:8080 -v "$HOME/.ai-server/models:/models" \
  ghcr.io/k0in/self:latest serve kev:4b --host 0.0.0.0 --port 8080
```

The directory layout is selected by the model id and quantization. Do not mount a single GGUF file over `/models`; mount the directory so `self` can verify, resume, and reuse its downloads.
