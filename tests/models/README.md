# Model tests

## Without models (`go test ./...`)

- `models_test.go` checks every model in the source registry: default quant,
  adapter, file sizes and sha256 pins. It downloads nothing.
- `harness_test.go` tests the checks below against a fake engine behind the real
  HTTP stack: they must pass on a healthy engine and fail on a broken one.

## With the real engines

```bash
just test-models                  # every decision model, default quant
just test-model kev:0.5b          # one model
just test-model kev:4b@q8         # one quant
just test-model 'decider*'        # a glob
```

Both build `self`, make sure the engines exist, then run `TestModels`. Each model
is downloaded once (files already in the model directory are reused), served
with the real `self serve` and engine, checked, and shut down. The models come
from the published registry, the one `self` uses.

Every check is a subtest, so a failure names the model and the check. The server
log is printed for a failed model. Filter with `go test` as usual:

```bash
SELF_TEST_MODELS=kev:0.5b go test ./tests/models -run 'TestModels/.*/choice' -v
```

Settings, all optional except `SELF_TEST_MODELS`:

| Variable | Default | Meaning |
| :--- | :--- | :--- |
| `SELF_TEST_MODELS` | unset (test skipped) | `all`, or comma-separated ids and globs, optionally `@quant` |
| `SELF_TEST_QUANTS` | `default` | `default`, `all`, or comma-separated quants |
| `SELF_TEST_DEVICE` | `auto` | passed to `self serve --device` (`cpu`, `cuda`, `vulkan`, ...) |
| `SELF_TEST_BIN` | `bin/self` | `self` binary |
| `SELF_TEST_ENGINE_DIR` | `bin/libexec/ai-server` | engine directory |
| `SELF_TEST_MODELS_DIR` | `AI_SERVER_MODELS` or `~/.ai-server/models` | model cache |
| `SELF_TEST_REGISTRY` | published registry | list models from this registry file instead |

## What is checked

Over HTTP, against one running server per model:

- `api`: health, `/v1/model` and `/v1/models` agree with the registry;
  malformed, unknown-field, wrong-model and wrong-content-type requests are
  rejected with the documented status and error type.
- the `self check` probes (`app.Probes`): obvious routing, option order swap,
  `noul` true and false, `score` ordering.
- `answers`: several questions in one request come back in request order,
  probabilities sum to 1 and name the options in order, non-ASCII input works.
- `limits`: exactly `max_options` options work (the right answer is the last
  option, which catches off-by-one letter readouts); one more is a 422.
- `queue`: concurrent identical requests all succeed and agree.
- `vision`: solid red, green and blue images are named correctly; a broken image
  is a 400; too many images is a 422. Text-only models must refuse images with a
  422.
- `lifecycle`: `self serve` exits with status 0 within 20 s of an interrupt.

The fixtures are meant to be easy: a model that fails one has a real problem
with its engine, adapter or weights, not a hard question. `just test-model` uses
the same probes as `self check`, so use that command to onboard a new model.
