# 15 · Known limits and open points

## Limits

- Decision models only.
- Decider engine: max 10 options, 1 image, plain layout only (no `"layout": "chat"`), temperature 1.0.
- Laya: max 16 options, text only.
- No multi-part (split) GGUFs.
- One model per server process.
- No auto restart after engine crash.
- sha256 checked on download only, not on every start.
- Engine build tested on Linux x86_64 (WSL2) only.

## Open points

- `self check` for `decider:0.8b` and `decider:4b`.
- Visual check of the registry site.
- Model cards are generated stubs. Need real text.
- macOS / Windows engine builds.
- Engine crash seen once during manual testing (server on :8080, cause unknown).
  - Likely the engine binary was replaced by a rebuild while running.
  - Check with `just serve-verbose` if it happens again.
