# Model tests and benchmarks

`models_test.go` checks every model in the source registry without downloading
weights. It runs as part of `go test ./...`.

For a local end-to-end test of every supported model, build the server and
engines, then run:

```bash
just setup
SELF_MODEL_TEST=1 go run ./tests/modelbench -bin ./bin/self -out .tmp/model-benchmark
```

The runner discovers models from the same published registry used by the main
app. Pass `-registry models/registry.yml` when deliberately testing a local
registry checkout.

For each model, the runner starts the main `self serve` app, waits for its HTTP
API, verifies `/health` and `/v1/model`, then posts the same System One request
to `/v1/systemone`. It checks that the response has the expected model ID,
answer shape, valid confidence, and the expected `billing` decision for the
canonical "I was charged twice" fixture. It uses the normal model cache and
writes:

- `model-benchmark.json` for automation
- `model-benchmark.md` for a human-readable report

Use `MODEL_TEST_STOP_ON_ERROR=1` to stop after the first failed model. The
benchmark measures end-to-end startup and API duration. It is intended for
local hardware comparisons and developer acceptance checks, not as a CI timing
gate.