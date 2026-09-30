# 02 · CLI

## Commands

| Command | Does |
| :--- | :--- |
| `self serve <model>` | Download if needed, start engine, serve HTTP |
| `self pull <model>` | Download only, print the local file paths on stdout (registry order, model file first) |
| `self ls` (`list`) | Downloaded models: quant, type, adapter, size, pinned sha256 (short, one per file), capabilities, description |
| `self ls-remote` (`list-remote`) | Registry models grouped by type: quants (`*` marks the default), size of the default quant, capabilities, description |
| `self rm <model>[@quant]` (`remove`) | Delete downloaded files; all quants unless one is given |
| `self check <model>` | Start engine + probe questions (see 11) |
| `self benchmark <model>` | Time the model on this machine, write a JSON report, print a link to share it |
| `self settings <model>` | Effective engine settings + source of each value |
| `self completion bash\|zsh\|fish` | Print a shell completion script |

No arguments (or `self help`) prints the usage text and exits 0.

`pull`, `check`, `benchmark`, and `settings` accept the same flags as `serve`
(`benchmark` adds its own, below). `ls`,
`ls-remote`, and `rm` only take `--models-dir` (`rm` also `--quant`).

## Flags

- `--host` (127.0.0.1), `--port` (8080), `--quant`, `--models-dir` (~/.ai-server/models).
- `--device`: `auto | cpu | cuda | cuda:N | metal | vulkan | vulkan:N`.
- `--queue-size` (64, 1-100000), `--preprocess-concurrency` (8, 1-1024).
- `--runtime-dir`, `--allow-http-images`, `--allow-private-images`,
  `--verbose` / `-v`.
- `--set key=value` (repeatable): engine setting override, highest precedence (see 06).
- `--settings-file FILE`: local settings (default `~/.ai-server/settings.yml`).
- Flags may come before or after the model reference. Exactly one model is required.

## `self benchmark <model>`

Starts the engine in-process like `check` (no HTTP server), times a fixed set of
requests one at a time, and writes a report others can compare with.

```bash
self benchmark kev:0.5b
self benchmark decider:4b@q8 --device cuda --iterations 50 --out reports/
self benchmark kev:4b --gpu "AMD Radeon RX 7900 XTX" --device vulkan
```

- Extra flags: `--iterations N` (timed requests per scenario, 20), `--warmup N` (untimed, 2),
  `--out PATH` (file or directory, default the current directory), `--gpu NAME` (record a GPU
  that cannot be detected; only NVIDIA GPUs are).
- Steps: download if needed, sha256 of every model file (must match the registry pin), start
  the engine, the `self check` probes (result goes into the report), the scenarios, write the
  report.
- Scenarios (`internal/benchmark`, `Version` 1): `short-choice`, `multi-question`,
  `max-options` (the model's limit, the right answer last), `long-context` (half of the usable
  context, at most 2048 tokens; a quarter of the iterations, at least 3) and `vision` (one
  448x448 image; half of the iterations, at least 5). Change a request or a scenario -> bump
  `Version`, reports of different versions are not comparable.
- Per scenario: latency min/mean/p50/p95/max as the caller sees it and as the engine reports it
  (`usage.latency_ms`), requests per second and input tokens per second (tokens divided by the
  summed request time; `output_tokens` only when the engine reports them).
- Report (`schema` 1): model id, quant, adapter, files with size and sha256, settings; CPU,
  threads, RAM, GPUs with driver; requested and engine device, `gpu_memory_mib` and `gpu_used`;
  load time; probes passed; `self` commit. No host name, user name or paths.
- `gpu_used` is true when the run did not ask for the CPU and loading added at least a quarter
  of the model size in NVIDIA memory. A silent CPU fallback (missing GPU runtime) prints a
  warning and the report is filed under the CPU (`cpu-<cpu>`), not under the GPU.
- File name: `<model>-<quant>-<rig>-<UTC time>.json`, e.g.
  `kev-0.5b-q4-rtx-5090-20260930-153012.json`. The report path goes to stdout, all other
  output to stderr.
- Sharing: the command prints a `github.com/K0IN/self/new/main?filename=benchmarks/<file>&value=<report>`
  link (GitHub opens a pull request from the user's fork on "Propose changes") and the
  `.../upload/main/benchmarks` page for uploading the file by hand. Reports go flat into
  `benchmarks/`; `go test ./internal/benchmark` validates every file there (schema, hashes,
  consistent statistics, file name matches the contents).

## Model references

CLI model references use `<model>:<size>@<quant>`. The `@<quant>` suffix is
optional and selects the registry default when omitted:

```bash
self serve kev:0.5b
self serve kev:4b@q8
self serve decider-vision:2b@q4
self pull kev:4b@fp16
```

The registry ID remains the portion before `@`, so registry keys and local
settings continue to use IDs such as `decider-vision:2b`. Do not combine an
`@<quant>` suffix with `--quant` in the same command.

The `justfile` provides a few development wrappers (see 13 for all recipes):

- `just setup`: build `self`, fetch the Laya runtime, and build the decision
  runtime.
- `just build`: build `bin/self` only.
- `just serve [model] [flags]`: build/bootstrap and then run
  `bin/self serve <model> --port $PORT <flags>` (default model `kev:0.5b`,
  overridable with `MODEL`).
- Everything else (`pull`, `ls`, `check`, `settings`) is run directly, for
  example `bin/self check kev:0.5b`.

## Config rules

- Precedence: CLI > env > defaults.
- Env: `AI_SERVER_HOST`, `AI_SERVER_PORT`, `AI_SERVER_MODELS`, `AI_SERVER_RUNTIME_DIR`, `AI_SERVER_SETTINGS`, `HF_TOKEN`, `HF_ENDPOINT`.
  `AI_SERVER_MODELS` also applies to `ls`, `ls-remote`, and `rm`.
- Terminal UX (progress, status) goes to stderr. stdout stays clean, so
  `self pull` output can be scripted; `ls`, `ls-remote`, and `settings` write
  their tables to stdout.

## Startup output

```
Model    kev:0.5b
Quant    q4
Adapter  ggmlc-laya
Settings threads=4

<download progress, only when files are missing>
Using ~/.ai-server/models/kev/0.5b/q4/kev_0.5b_ud_q4_k_m.gguf

Loading model...
Ready

Engine   <path to engine>
Device   cpu
VRAM     1212 MiB (NVIDIA GPUs)
API      http://127.0.0.1:8080
```

- `Settings` is omitted when there are none.
- `VRAM` is total used NVIDIA memory from `nvidia-smi`, or `n/a`.
- On Ctrl+C / SIGTERM it prints `Shutting down...` and exits 0.

## Acceptance criteria

- Must: an unknown command prints `Error [internal_error]: unknown command ...`, exit 1.
- Must: errors print `Error [<kind>]: <message>`, exit 1.
- Must: Ctrl+C during a download keeps the `.part` file, exit 130.
- Must: `self pull` prints only paths on stdout (scriptable).
- Must: CLI flag beats env, env beats default (`internal/config` tests).
- Must: `<model>@<quant>` and `--quant` together are rejected (`TestModelReferenceQuant`).
