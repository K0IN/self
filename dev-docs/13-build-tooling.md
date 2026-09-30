# 13 · Build tooling

## Local workflow

The repository uses [`just`](https://just.systems) (with modules) for repeatable
build and runtime tasks. `just` with no arguments lists the public recipes.

```text
just setup      # bin/self + Laya engine + Decider engine
just serve      # serve kev:0.5b (build + bootstrap first)
just check      # gofmt + go vet + go test
```

Tools: Go (see `go.mod`), `just`, `curl`, `tar`. The Decider engine also needs
`cmake` and `ninja`; `just site` needs Node.js and `npm`. The dev container
(`.devcontainer/`) has all of them (CUDA 13 devel image, fish, `--gpus=all`) and
aliases `self` to `go run ./cmd/self`.

`just setup` builds `bin/self`, downloads the upstream Laya runtime, and builds
the bundled custom decision runtime. `just bootstrap` (private) checks the
installed runtimes and builds only missing ones (Laya is re-fetched when it does
not start). Runtime artifacts are placed under `bin/libexec/ai-server/`.

## `just` (root `justfile`)

| Recipe | Does |
| :--- | :--- |
| `just setup` | `build` + `runtime` + `engine-decider` |
| `just build` | `go build -trimpath -o bin/self ./cmd/self` |
| `just serve [model] [flags]` | `build` + `bootstrap`, then `bin/self serve <model> --port $PORT <flags>` |
| `just check` | `gofmt -l` must be empty, then `go vet ./...` and `go test ./...` |
| `just test-models` | `build` + `bootstrap`, then the end-to-end API tests for every model (downloads each once, see 14) |
| `just test-model <model>` | Same for one model, e.g. `kev:0.5b` or `kev:4b@q8` |
| `just fmt` | `gofmt -w .` |
| `just site` | Build the documentation site into `site/` (see 12) |
| `just site-serve` | `just site`, then preview on port 8000 |
| `just runtime [variant]` (private) | `just engine runtime <variant> $GGMLC_VERSION`: download upstream Laya + bundle CUDA libs |
| `just engine-decider [variant]` (private) | `just engine build <variant>`: build our Decider engine |
| `just bootstrap` (private) | Build only missing runtimes |

Private recipes are hidden from `just --list` but can be run by name. There are
no wrappers for `pull`, `ls`, `check`, or the HTTP smoke requests; use
`bin/self` and `curl` directly. There is no `just test`; use `go test ./...`
(or `just check`). The end-to-end model tests are `just test-models` and
`just test-model <model>` (see 14).

Environment: `GGMLC_VERSION` (Laya release, default `v0.9.6`), `MODEL` (default
`kev:0.5b`), `PORT` (default `8080`), `LLAMA_CPP_TAG` (default `b11256`),
`CUDA_LIB_DIRS` (extra directories searched for CUDA 12 libraries).

## Engine build (`engines/`)

- `mod engine "engines"` in the root justfile. `engines/justfile` is the dispatcher and
  declares the modules `decider` (`engines/ggmlc-custom-decider/justfile`) and `laya`
  (`engines/laya/justfile`). List: `just --list engine`.
- No bash scripts.

| Recipe | Does |
| :--- | :--- |
| `just engine deps [variant]` | Fetch prebuilt llama.cpp libs + headers into `engines/ggmlc-custom-decider/deps/` |
| `just engine compile [variant]` | cmake + ninja into `build/` |
| `just engine build [variant] [target]` | compile + install to `bin/libexec/ai-server`, then verify |
| `just engine verify [target]` | ldd check for missing libs |
| `just engine clean` | Remove `build/`, `deps/` |
| `just engine runtime [variant] [version]` | Fetch upstream Laya (= `just engine laya fetch`) |

The same recipes are reachable as `just engine decider <recipe>` and
`just engine laya fetch`.

- Decider variants: `cuda-12.8` (default), `vulkan`, `cpu`.
- llama.cpp version: `LLAMA_CPP_TAG` (default `b11256`).
- `deps/` skipped if tag + variant unchanged (`deps/.tag`).
- `verify` only warns about libraries it cannot resolve.

## Runtime (`just runtime`)

- Downloads laya (`GGMLC_VERSION`, default v0.9.6) for `auto | cuda-sm80 | cuda-sm86 | cuda-sm89 | vulkan | metal`
  from monatis/ggmlc releases (Linux x86_64, or macOS arm64 where it always selects `metal`).
- `auto` picks the CUDA build from `nvidia-smi` compute capability (80/87 -> `cuda-sm80`,
  86 -> `cuda-sm86`, 89/90 -> `cuda-sm89`); anything else, including sm120, uses `vulkan`.
  CUDA builds need `libcudart.so.12` and `libcublas.so.12` (or `CUDA_LIB_DIRS`), otherwise it falls back to Vulkan.
  `auto` fails when neither CUDA 12 nor `libvulkan.so.1` is available.
- CUDA builds get the missing `libcudart` / `libcublas` copied into `lib/`.
- The recipe finishes by running `laya help` and prints `Engine OK` or a warning.

## Bundle layout

```
self
libexec/ai-server/{laya, ggmlc-custom-decider, lib/}
```

In a checkout this is `bin/self` + `bin/libexec/ai-server/`. There is no
release tarball recipe; releases are container images.

## Container images

- `docker/Dockerfile` copies a prebuilt `bin/self` and `bin/libexec` into `/app`, so run
  `just setup` (or the CI steps below) first. Build arg `CUDA_VARIANT` selects the base:
  `cuda-13` (`nvidia/cuda:13.4.1-runtime`), `cuda-12` (`12.8.1-runtime`), or `cpu` (`ubuntu:24.04`).
- Defaults: `AI_SERVER_RUNTIME_DIR=/app/libexec/ai-server`, `AI_SERVER_MODELS=/models` (volume),
  `AI_SERVER_HOST=0.0.0.0`, `AI_SERVER_PORT=8080`; entrypoint `/app/self`, default command `serve kev:0.5b`.
- `.github/workflows/container-publish.yml` (push to `main`, tags `v*.*.*`, manual dispatch) builds three
  variants, each after `go test ./...`:

  | Variant | Base | Decider engine | Laya |
  | :--- | :--- | :--- | :--- |
  | `cuda-13` | CUDA 13 | `cpu` | `vulkan` |
  | `cuda-12` | CUDA 12 | `cuda-12.8` | `vulkan` |
  | `cpu` | Ubuntu 24.04 | `cpu` | `vulkan` |

- Tags on `ghcr.io/<repo>`: `<variant>-<commit>`, `<variant>` (main only), `latest` (`cuda-13`, main only),
  `<variant>-<semver>` (version tags).
- The workflow pins `just` 1.43.0 and passes `--unstable` for modules.

## Build output

`bin/`, `site/`, `*.part`, and a root `dist/` are ignored by git. `just engine clean`
removes engine `build/` and `deps/`. Downloaded models (`~/.ai-server/models`)
are never touched by the build.

## Acceptance criteria

- Must: `just setup` on a clean checkout gives a working `bin/`.
- Must: `just engine build` and `just engine-decider` both succeed (verified: engines present in `bin/libexec/ai-server`).
- Must: `just engine verify` reports "All shared libraries resolved".
- Must: `just check` passes (gofmt clean, vet, tests; verified).
- Must: the container workflow builds all three variants.
