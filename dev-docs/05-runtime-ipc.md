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

## Models stay loaded

- `self serve` starts one engine process per served model and keeps it for the whole run; a
  request never starts a process or loads weights. The ready handshake only completes once the
  weights, the context and the projector are loaded, so the first request is as warm as the rest
  (apart from a few hundred ms of one-time GPU kernel setup).
- Engines (`ggmlc-custom-decider`, `clef`, `ggmlc-audio`; Laya is a daemon)
  answer request frames in a loop. Text models use the upstream `llama-server`
  child over HTTP on a private Unix domain socket, with private loopback TCP
  on Windows. The only per-request model state reset is the KV cache.
- On non-Windows systems, `self` passes the socket path to the unmodified
  upstream `llama-server` via `--host`. The socket lives in a `0700` temporary
  directory, and the HTTP client connects using Unix `DialContext`. After the
  child exits, idle HTTP connections are closed and the directory is removed.
  The directory is also removed if the child fails to start.
- If requests are slow while the engine stays up, it is compute, not loading: check the device
  first (`Device` line at startup, or `ps` showing the same engine PID and a growing elapsed time
  while latency stays flat; see 13 for a bundle that silently runs on the CPU).
- Reference (`clef-flash:9b`, 828 tokens with one image): about 0.35 s on an RTX 5090, about 30 s on
  16 CPU cores.

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
- Must: custom decision/audio engines use SELFIPC1; the upstream `llama-server`
  text engine uses HTTP over a private Unix domain socket in a `0700` temporary
  directory, removed after child exit. Windows falls back to private loopback
  TCP. Neither internal endpoint is exposed through the public API.
