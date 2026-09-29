# 02 · CLI

## Commands

| Command | Does |
| :--- | :--- |
| `self serve <model>` | Download if needed, start engine, serve HTTP |
| `self decision <model>` | Same, but refuses non-decision models |
| `self pull <model>` | Download only, print local paths on stdout |
| `self list` | Models, quants, size, capabilities, downloaded, description |
| `self onboard <hf-repo>` | Print a registry entry (see 11) |
| `self check <model>` | Start engine + probe questions (see 11) |
| `self settings <model>` | Effective engine settings + source of each value |

## Flags

- `--host` (127.0.0.1), `--port` (8080), `--quant`, `--models-dir` (~/.ai-server/models).
- `--device`: `auto | cpu | cuda | cuda:N | metal | vulkan | vulkan:N`.
- `--queue-size` (64), `--preprocess-concurrency` (8).
- `--runtime-dir`, `--registry`, `--allow-http-images`, `--allow-private-images`, `-v`.
- `--set key=value` (repeatable): engine setting override, e.g. `--set context_size=4096`.
- `--settings-file FILE`: local settings (default `~/.ai-server/settings.yml`).

## Config rules

- Precedence: CLI > env > defaults.
- Env: `AI_SERVER_HOST`, `AI_SERVER_PORT`, `AI_SERVER_MODELS`, `AI_SERVER_RUNTIME_DIR`, `AI_SERVER_SETTINGS`, `HF_TOKEN`, `HF_ENDPOINT`.
- Terminal UX (progress, status) goes to stderr. stdout stays clean.

## Startup output

```
Model    kev:0.5b
Quant    4bit
Adapter  ggmlc-laya
Using ~/.ai-server/models/kev/0.5b/4bit/...
Loading model...
Ready
API      http://127.0.0.1:8080
```

## Acceptance criteria

- Must: unknown command prints usage, exit 2.
- Must: errors print `Error [<kind>]: <message>`, exit 1.
- Must: Ctrl+C during download keeps the `.part` file, exit 130.
- Must: `self pull` prints only paths on stdout (scriptable).
- Must: CLI flag beats env, env beats default (`internal/config` tests).
