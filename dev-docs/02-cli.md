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
- `--runtime-dir`, `--allow-http-images`, `--allow-private-images`,
  `--verbose` / `-v`.
- `--settings-file FILE`: local settings (default `~/.ai-server/settings.yml`).

## Model references

CLI model references use `<model>:<size>@<quant>`. The `@<quant>` suffix is
optional and selects the registry default when omitted:

```bash
self serve kev:0.5b
self serve kev:4b@8bit
self serve decider-vision:2b@4bit
self pull kev:4b@f16
```

The registry ID remains the portion before `@`, so registry keys and local
settings continue to use IDs such as `decider-vision:2b`. Do not combine an
`@<quant>` suffix with `--quant` in the same command.

The `justfile` provides the usual development wrappers:

- `just setup`: build `self`, fetch the Laya runtime, and build the decision
	runtime.
- `just bootstrap`: build only the runtimes missing from `bin/libexec/ai-server`.
- `just serve`, `just serve-verbose`, `just serve-cpu`, and `just decision`:
  build/bootstrap and then run the matching command.
- `just pull`, `just list`, `just check-model`, and `just settings`: invoke the
  corresponding CLI workflows.

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
