# 05 · Runtime and IPC

## Engine discovery

- Engines live in `libexec/ai-server/` next to the binary (`<exe dir>/libexec/ai-server`, then `<exe dir>/../libexec/ai-server`).
- Override: `--runtime-dir` (env `AI_SERVER_RUNTIME_DIR`); when set it is the only location searched.
  Use an absolute path: the engine starts with its own directory as working directory.
  The container image sets it to `/app/libexec/ai-server`.
- Development fallback, tried last: `<checkout>/bin/libexec/ai-server`, so `go run ./cmd/self` finds
  what `just setup` (or `just runtime` / `just engine-decider`) installed. It comes from the source path compiled into the binary, so it is
  absent from `-trimpath` builds (`just build`, releases, the container image).
- Never searched on `$PATH`.
- Not found -> `runtime_not_found` listing the directories searched. A file without the execute bit is also `runtime_not_found`.
- `lib/` next to the engine holds CUDA / llama.cpp shared libs. It is prepended to `LD_LIBRARY_PATH` for the child only
  (`DYLD_LIBRARY_PATH` on macOS, `PATH` on Windows).

## Process lifecycle

- Child gets its own process group (`Setpgid`).
- Linux: `Pdeathsig=SIGKILL`. Engine dies if `self` dies.
- stdin = requests, stdout = responses, stderr = logs.
- stderr shown with `-v` (prefixed `[engine]`). The last 200 lines are kept; up to 15-20 are attached to crash errors.
- Stop: close stdin -> wait 5 s -> SIGTERM -> wait 3 s -> SIGKILL.
- A request that gets no answer within 2 minutes kills the engine; the request fails with `runtime_crashed`.
- No auto restart. Leave that to systemd / Podman / k8s.

## SELFIPC1 framing (`internal/ipc`)

```
"SELFIPC1" | be32 header_len | JSON header | be32 n | (be64 len | bytes) × n
```

- Same format both ways.
- Header: `{"id", "method", "params"}` (request) or `{"id", "result" | "error"}` (response). `id` is a non-zero integer echoed by the engine. The only method is `systemone`.
- `error` is `{"type", "message"}`. Type `invalid_request` becomes a 400; anything else is `internal_error`.
- Attachments: raw bytes (RGB8 images). No base64.
- Limits (`ipc.DefaultLimits`): header 4 MiB, 32 attachments, 128 MiB per attachment, 512 MiB per frame. Oversized, truncated, or malformed frames are rejected.
- The engine sends one handshake frame first, without `id`: `{"type":"ready","protocol":1,"model","device","capabilities"}`.
  Capabilities: `text`, `choice`, `score`, `noul`, `max_options`, and `vision` (`enabled`, `max_images`, `input` geometry). The message shapes are documented in `internal/adapters/selfipc/adapter.go`.
- Engine stdout carries frames only. The C++ engine points fd 1 at stderr so library output cannot corrupt the stream.

## Acceptance criteria

- Must: frames round-trip; oversize / truncated frames are rejected (`internal/ipc` tests).
- Must: engine not found -> `runtime_not_found`. Fails to start -> `runtime_start_failed`.
- Must: engine exit during serve -> current request `runtime_crashed`, queue rejected, `self` exits non-zero (`TestCrashAndProtocolErrors`, `TestSchedulerCrashFailsQueued`).
- Must: no engine process left after `self` exits (manual, verified: no engine process after SIGINT).
- Must: no localhost HTTP between server and engine.
