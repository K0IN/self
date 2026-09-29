# 05 · Runtime and IPC

## Engine discovery

- Engines live in `libexec/ai-server/` next to the binary (or `../libexec/ai-server`).
- Override: `--runtime-dir`.
- Never searched on `$PATH`.
- `lib/` next to the engine holds CUDA / llama.cpp shared libs. `LD_LIBRARY_PATH` is set for the child only.

## Process lifecycle

- Child gets its own process group (`Setpgid`).
- Linux: `Pdeathsig=SIGKILL`. Engine dies if `self` dies.
- stdin = requests, stdout = responses, stderr = logs.
- stderr shown with `-v`. Last lines attached to crash errors.
- Stop: close stdin -> wait -> SIGTERM -> SIGKILL.
- No auto restart. Leave that to systemd / Podman / k8s.

## SELFIPC1 framing (`internal/ipc`)

```
"SELFIPC1" | be32 header_len | JSON header | be32 n | (be64 len | bytes) × n
```

- Same format both ways.
- Header: `{"id", "method", "params"}` or `{"id", "result" | "error"}`.
- Attachments: raw bytes (RGB8 images). No base64.
- Strict size limits on header and attachments.
- Engine sends a `ready` frame first with capabilities + image geometry.

## Acceptance criteria

- Must: frames round-trip; oversize / truncated frames are rejected (`internal/ipc` tests).
- Must: engine not found -> `runtime_not_found`. Fails to start -> `runtime_start_failed`.
- Must: engine exit during serve -> current request `runtime_crashed`, queue rejected, `self` exits non-zero.
- Must: no engine process left after `self` exits (manual: "engine reaped" in e2e).
- Must: no localhost HTTP between server and engine.
[text](../internal)