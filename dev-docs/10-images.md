# 10 · Images

## Pipeline

`URL / data URI -> memory -> decode -> EXIF orientation -> resize -> RGB8 -> IPC attachment`

- Never written to disk.
- Formats: JPEG, PNG, WebP. The format is detected from the bytes; a declared `Content-Type` that disagrees is rejected (`unsupported_image`).
- Data URIs must be base64.
- EXIF orientation applies to JPEG.
- Target geometry comes from the engine's `ready` frame (`fixed`, `bounded`, `dynamic`). Never from model names.
  - `bounded`: scale down to fit `max_width` x `max_height`, never up (the Decider engine: 1536 x 1536, `contain`).
  - `fixed`: exactly `width` x `height`. `dynamic`: Qwen-VL style rounding into `min_pixels` .. `max_pixels`.
  - Resize mode `contain` (letterbox on black, default), `cover` (center crop), or `stretch`. Alpha is composited onto black.

## Limits

- 15 s timeout, 3 redirects.
- 20 MiB source, 40 MP decoded, 12000 px per side.
- Header size checked before full decode.
- Max images per request from capabilities (1 for the Decider engine).
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

## Acceptance criteria

- Must: oversize image -> 413. Bad format -> 400. Fetch error -> 502.
- Must: private / loopback URLs rejected by default (`TestPrivateAddressBlocked`).
- Must: EXIF rotation applied (`TestOrientation`).
- Must: geometry modes and resize behave (`TestTargetSize`, `TestResizeContainPreservesAspect`).
- Must: no image bytes on disk.
- Must: solid-color images are recognised by vision models (end-to-end test `vision: solid colors`, see 14).
