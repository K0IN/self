# 10 · Images

## Pipeline

`URL / data URI -> memory -> decode -> EXIF orientation -> resize -> RGB8 -> IPC attachment`

- Never written to disk.
- Formats: JPEG, PNG, WebP.
- Target geometry comes from the engine's `ready` frame (`fixed`, `bounded`, `dynamic`). Never from model names.

## Limits

- 15 s timeout, 3 redirects.
- 20 MiB source, 40 MP decoded, 12000 px per side.
- Header size checked before full decode.
- Max images per request from capabilities.

## Security

- `https://` only by default (`--allow-http-images` to relax).
- Private, loopback, link-local, CGNAT blocked after DNS resolution.
- Also checked on redirects (covers DNS rebinding).
- `--allow-private-images` to relax. `file://` never.

## Latency note

- URL images add fetch time (httpbin: 150–590 ms).
- Data URIs skip that (~50 ms total for decider vision).

## Acceptance criteria

- Must: oversize image -> 413. Bad format -> 400. Fetch error -> 502.
- Must: private / loopback URLs rejected by default (`internal/imageutil` tests).
- Must: EXIF rotation applied.
- Must: no image bytes on disk.
- Manual: README example URLs work (gstatic 1.webp / 4.webp, httpbin jpeg / png).
