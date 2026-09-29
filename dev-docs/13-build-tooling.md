# 13 · Build tooling

## Local workflow

The repository uses `just` for repeatable build and runtime tasks. The default
recipe lists available commands.

```text
just setup
just build
just bootstrap
just test
just test-race
just check
```

`just setup` builds `bin/self`, downloads the upstream Laya runtime, and builds
the bundled custom decision runtime. `just bootstrap` checks the installed
runtimes and builds only missing ones. Runtime artifacts are placed under
`bin/libexec/ai-server/`.

## `just` (root `justfile`)

| Recipe | Does |
| :--- | :--- |
| `just setup` | build + runtime + engine-decider |
| `just build` | `bin/self` |
| `just bootstrap` | Build only missing runtimes |
| `just runtime [variant]` | Download upstream laya + bundle CUDA libs |
| `just engine-decider [variant]` | Build our Decider engine |
| `just serve / serve-verbose / serve-cpu [model]` | Run server |
| `just pull`, `just list`, `just decision` | CLI shortcuts |
| `just onboard <repo>`, `just check-model <id>` | Onboarding |
| `just health`, `just model-info`, `just try-text`, `just try-all` | Try the API |
| `just test`, `test-race`, `test-integration`, `check`, `fmt` | Tests |
| `just site`, `site-serve` | Registry site |
| `just release` | `dist/self-<os>-<arch>.tar.gz` |
| `just clean` | Remove build output |

## Engine build (`engines/ggmlc-custom-decider/justfile`)

- Imported as module: `mod engine`. List: `just --list engine`.
- No bash scripts.

| Recipe | Does |
| :--- | :--- |
| `just engine deps [variant]` | Fetch llama.cpp libs + headers into `deps/` |
| `just engine compile [variant]` | cmake + ninja into `build/` |
| `just engine build [variant] [target]` | compile + install to `bin/libexec/ai-server` |
| `just engine verify` | ldd check for missing libs |
| `just engine clean` | Remove `build/`, `deps/` |

- Variants: `cuda-12.8` (default), `vulkan`, `cpu`.
- llama.cpp version: `LLAMA_CPP_TAG` (default `b11256`).
- `deps/` skipped if tag + variant unchanged (`deps/.tag`).

## Runtime (`just runtime`)

- Downloads laya (`GGMLC_VERSION`, default v0.9.6) for `auto | cuda-sm80 | cuda-sm86 | cuda-sm89 | vulkan`.
- Bundles `libcudart` / `libcublas` 12 into `lib/`.

The Laya download recipe accepts `auto`, `cuda-sm80`, `cuda-sm86`, `cuda-sm89`,
or `vulkan`. The custom decision engine accepts `cuda-12.8`, `vulkan`, or
`cpu`.

Use `GGMLC_VERSION` to select the upstream Laya release. `MODEL` and `PORT`
provide defaults for the serving recipes.

## Release layout

```
self
libexec/ai-server/{laya, ggmlc-custom-decider, lib/}
```

`just release` creates `dist/self-<os>-<arch>.tar.gz` containing the binary and
runtime directory. `just site` installs the documentation-site dependencies
and renders the registry overview into `site/`; `just site-serve` serves the
rendered site at `http://127.0.0.1:8000`.

The generated `site/` directory is build output. `just clean` removes it,
`bin/self`, and `dist/`, while retaining downloaded models.

## Acceptance criteria

- Must: `just setup` on a clean checkout gives a working `bin/`.
- Must: `just engine build` and `just engine-decider` both succeed (verified).
- Must: `just engine verify` reports "All shared libraries resolved".
- Must: `just release` produces a self-contained tarball.
