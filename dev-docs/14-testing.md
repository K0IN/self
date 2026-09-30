# 14 · Testing

## Commands

- `go test ./...` -> all Go unit tests (no GPU, no model, no network for the unit tests).
- `go test -race ./...` -> with the race detector.
- `just check` -> `gofmt -l` must print nothing, then `go vet ./...` and `go test ./...`.
- `just fmt` -> `gofmt -w .`
- End-to-end model tests (real `self`, real engines, downloads each model once): `just test-models` for every model, `just test-model kev:0.5b` for one (see below).
- Integration test against a real Laya engine: see below.

There are no `just test`, `just test-race`, or `just test-integration` recipes.

## Unit tests (no GPU, no model)

| Package | Covers |
| :--- | :--- |
| `internal/config` | Precedence, model references with `@quant`, `--set`, invalid values |
| `internal/registry` | Parse, resolve, rejects, pins, readme, capabilities, unknown types skipped, fetch, bundled registry |
| `internal/models` | Resume, retries, stall, verify, lock, pins, store layout, `rm`, registry cache |
| `internal/ipc` | Framing, limits, response ids |
| `internal/decision` | Question/answer parsing, capabilities, scheduler, cancellation |
| `internal/api/decision` | Decision HTTP handlers with a fake adapter |
| `internal/imageutil` | Decode, limits, resize, orientation, SSRF |
| `internal/adapters` | Settings layers and rejection, bundled registry settings |
| `internal/adapters/ggmlclaya` | GGUF compatibility, settings args, response translation |
| `internal/adapters/selfipc` | Handshake, crash, protocol errors against a fake engine (the test binary re-executes itself when `SELFIPC_FAKE` is set) |
| `internal/adapters/customdecider` | Model check, engine args |
| `internal/settings`, `internal/localconf` | Schema validation, local settings file |
| `internal/runtime` | Engine search order; `Supervisor` handshake, watchdog and crash reports against a fake engine (the test binary re-executes itself when `RUNTIME_TEST_ENGINE` is set) |
| `internal/onboard` | Bucketing, adapter pick, YAML valid |
| `internal/app` | Registry load and offline cache fallback |
| `tests/models` | Source registry sanity (default quant, adapter, pins); model selection; the end-to-end checks against a fake engine behind the real HTTP stack |
| `internal/benchmark` | Statistics and throughput, scenario choice per capability, report validation, file names, share links, GPU-used verdict, and every report checked into `benchmarks/` |

No tests: `cmd/*`, `internal/api` (shared router; covered through `internal/api/decision`),
`internal/api/image`, `internal/errs`, `internal/ggufmeta`, `internal/jsonx`. The `serve` and `check`
wiring in `internal/app` is only exercised by the end-to-end model tests and manual runs.

## Integration test

```bash
SELF_TEST_ENGINE=$PWD/bin/libexec/ai-server/laya \
SELF_TEST_GGUF=$HOME/.ai-server/models/kev/0.5b/q4/kev_0.5b_ud_q4_k_m.gguf \
SELF_TEST_DEVICE=cpu \
  go test ./internal/adapters/ggmlclaya -run Integration -v
```

- Skipped when `SELF_TEST_ENGINE` or `SELF_TEST_GGUF` is missing. `SELF_TEST_DEVICE` defaults to `auto`.

## End-to-end model tests (`tests/models`)

`TestModels` downloads each selected model once, starts the real `self serve`
with the real engine, and checks the API. It is skipped unless
`SELF_TEST_MODELS` is set.

```bash
just test-models                    # every model, default quant
just test-model kev:0.5b            # one model (also kev:4b@q8, 'decider*')
SELF_TEST_MODELS=kev:0.5b SELF_TEST_DEVICE=cpu \
  go test ./tests/models -run 'TestModels/.*/choice' -v -timeout 0   # filter checks by hand
```

The recipes build `self`, make sure the engines exist and run
`go test ./tests/models -run TestModels -v -count=1` with `SELF_TEST_MODELS` set.

| Variable | Default | Meaning |
| :--- | :--- | :--- |
| `SELF_TEST_MODELS` | unset (test skipped) | `all`, or comma-separated ids / globs, optionally with `@quant` (`kev:*`, `decider:4b@q8`) |
| `SELF_TEST_QUANTS` | `default` | `default`, `all`, or a comma-separated list of quants |
| `SELF_TEST_DEVICE` | `auto` | `--device` passed to `self serve` |
| `SELF_TEST_BIN` | `../../bin/self` | `self` binary (build it with `just build`) |
| `SELF_TEST_ENGINE_DIR` | `../../bin/libexec/ai-server` | `--runtime-dir` |
| `SELF_TEST_MODELS_DIR` | `$AI_SERVER_MODELS` or `~/.ai-server/models` | model store; missing models are pulled with `self pull` |
| `SELF_TEST_REGISTRY` | published registry | Registry file used only to select models; `self serve` still resolves against the published registry |

- Paths are made absolute (a relative `--runtime-dir` breaks the engine start, see 15).
- Use `-timeout 0` when running `go test` by hand: downloads and model loads exceed the default
  10 minute test timeout. The recipes set their own timeout (2 h and 1 h).
- One subtest per model and quant, one `self serve` for all its checks, then a
  graceful shutdown (exit status 0 within 20 s of an interrupt):
  - `api: health`, `api: model metadata` (agrees with the registry), `api: invalid requests rejected`
  - the `self check` probes (`app.Probes`)
  - `answers:` multi-question request, well-formed probabilities, unicode input
  - `limits:` exactly `max_options` options work, one more is a 422
  - `queue: concurrent requests`
  - `vision:` text-only models refuse images (422); solid red/green/blue images are named
    correctly; a broken image is a 400; too many images is a 422
  - `lifecycle: graceful shutdown`
- Checks that do not apply (for example vision checks on a text-only model) are skipped.
- The harness itself (checks, model selection) is tested in `go test ./tests/models` against a
  fake engine behind the real HTTP stack (`TestSuitePassesOnHealthyEngine`,
  `TestSuiteCatchesBrokenEngine`, `TestMetadataMismatchIsReported`, `TestSelectTargets`).
- Verified: `SELF_TEST_MODELS=kev:0.5b SELF_TEST_DEVICE=cpu` passes all applicable checks.

## Smoke requests

With a server running (`just serve`, port 8080):

```bash
curl -s localhost:8080/health
curl -s localhost:8080/v1/model
curl -s -X POST localhost:8080/v1/systemone -H 'content-type: application/json' \
  -d '{"state":"I was charged twice.","questions":{"refund":{"type":"noul","instructions":"Refund needed?"}}}'
```

## Manual e2e (verified on the dev container, CPU)

- `self check` passes for `kev:0.5b`, `decider-vision:2b`, `decider:0.8b`, `decider:4b`, `laya:english`;
  it fails the `noul: clearly true` probe for `laya:multilingual` and `laya:typed-decisions`.
- `self serve kev:0.5b`: `/health`, `/v1/model`, `/v1/models`, `/v1/systemone`, unknown route (400), wrong method (405);
  SIGINT prints `Shutting down...` and leaves no engine process.
- `self settings` validation: unknown key, wrong-adapter key, missing explicit settings file.
- All ten downloaded GGUF files match the registry sha256.
- A tampered registry pin is refused (`TestEnsureRefusesChangedUpstream`, `TestEnsureRejectsPinMismatch`).

## Acceptance criteria

- Must: `go test ./...` and `go vet ./...` pass (verified).
- Must: `gofmt -l .` is empty (verified).
- Must: race detector clean (`go test -race ./...`, verified).
