# 14 · Testing

## Commands

- `just test` -> `go test ./...`
- `just test-race` -> with race detector
- `just check` -> gofmt + vet + tests
- `just test-integration` -> real laya engine + kev:0.5b

## Unit tests (no GPU, no model)

| Package | Covers |
| :--- | :--- |
| `internal/config` | Precedence |
| `internal/registry` | Parse, resolve, rejects, pins, readme |
| `models` | Bundled registry valid, readmes exist |
| `internal/models` | Resume, retries, stall, verify, lock, pins, store |
| `internal/ipc` | Framing |
| `internal/decision` | Validation, scheduler, cancellation |
| `internal/api/decision` | Decision HTTP handlers with a fake adapter |
| `internal/api` / `internal/api/image` | Shared API infrastructure and future image handlers |
| `internal/imageutil` | Decode, limits, SSRF |
| `internal/adapters/*` | Laya compat, selfipc fake engine, decider spec |
| `internal/onboard` | Bucketing, adapter pick, YAML valid |
| `internal/site` | Pages, escaping, bundled registry |
| `internal/app` | Serve / check wiring |

## Integration test

```bash
SELF_TEST_ENGINE=$PWD/bin/libexec/ai-server/laya SELF_TEST_GGUF=/path/kev.gguf \
  go test ./internal/adapters/ggmlclaya -run Integration -v
```

- Skipped when env vars are missing.

## Manual e2e (done)

- kev:0.5b, kev:4b, decider:2b-vision served and answered.
- Vision examples, error cases, 8 concurrent requests, clean shutdown.
- `self check` for kev:0.5b, decider:2b-vision.
- Tampered registry pin refused.

## Acceptance criteria

- Must: `go test ./...` and `go vet ./...` pass.
- Must: `gofmt -l .` is empty.
- Must: race detector clean.
