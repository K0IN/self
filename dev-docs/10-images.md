# 10 · Images

## Vision input pipeline

`URL / data URI -> memory -> decode -> EXIF orientation -> resize -> RGB8 -> IPC attachment`

Vision models retain fixed, bounded and dynamic geometry from capabilities,
with contain, cover or stretch resizing. Decider's bounded geometry is
1536 x 1536; RGB vision inputs composite alpha onto black.

## Diffusion edit pipeline

`multipart file / URL / data URI -> memory -> decode -> EXIF orientation -> validate -> sd-server HTTP multipart`

- Never written to disk.
- Formats: JPEG, PNG, WebP. The format is detected from bytes; a declared `Content-Type` that disagrees is rejected (`unsupported_image`).
- Data URIs must be base64.
- EXIF orientation applies to JPEG.
- Edit inputs are decoded and validated before being sent to the persistent stable-diffusion.cpp HTTP server. The selected diffusion model controls its own preprocessing and dimensions.
- Multiple `image[]` references are supported for image models that implement multi-reference editing.

## Limits

- 15 s timeout, 3 redirects.
- 20 MiB source, 40 MP decoded, 12000 px per side.
- Header size checked before full decode.
- Max edit references: 8.
- The JSON request body is capped at 32 MiB (see 08).

## Security

- `https://` only by default (`--allow-http-images` to relax). URLs with credentials are refused.
- Private, loopback, link-local, multicast, CGNAT and other reserved ranges are blocked at dial time, after DNS resolution.
- Also checked on redirects (covers DNS rebinding); redirect targets must pass the scheme check too.
- Environment proxies are never used for image fetches.
- `--allow-private-images` to relax. `file://` never.

## Latency note

- URL images add the fetch time of the image server.
- Data URIs skip that.

## Image generation runtime

`sd-server` is started once during `self serve` and owns the loaded diffusion model. The Go API forwards OpenAI-compatible generation and edit requests to the loopback-only server. Use `--device cpu` for CPU builds; CUDA support is provided by building stable-diffusion.cpp against CUDA 12 or CUDA 13.

The executable is upstream's unmodified `examples/server` target, pinned at
`master-929-3f8527a`, not a custom C++ engine. `--eager-load` preloads weights.
The Go adapter drains stdout, supervises stderr and serializes HTTP calls.
Readiness polling stops if the child exits; CPU maps to both compute and
parameter CPU placement, and CUDA indices map to upstream device names.

Registered models are `qwen-image-2.1:7b` and `flux2-klein:9b`. Image-only
`vae` and `text-encoder` roles accept safetensors; diffusion and projector
files remain GGUF. Every artifact has revision, byte-size and SHA-256 pins.
Qwen 2.1 requires its own VAE and the Qwen3-VL-8B F16 projector for editing.

Build with `just engine image cpu`, `cuda-12.8` or `cuda-13.4`. CUDA variants
require the matching toolkit via `CUDA_HOME`. The official CPU binary builds
successfully. Weight-loading, actual inference and CUDA builds still require
target-hardware smoke tests. Go unit tests alone do not verify inference.

## Acceptance criteria

- Must: oversize image -> 413. Bad format -> 400. Fetch error -> 502.
- Must: private / loopback URLs rejected by default (`TestPrivateAddressBlocked`).
- Must: EXIF rotation applied (`TestOrientation`).
- Must: geometry modes and resize behave (`TestTargetSize`, `TestResizeContainPreservesAspect`).
- Must: no image bytes on disk.
- Must: generation returns OpenAI-style `data[].b64_json`.
- Must: multipart editing supports multiple references.
- Must: solid-color images are recognised by vision models (end-to-end test `vision: solid colors`, see 14).
