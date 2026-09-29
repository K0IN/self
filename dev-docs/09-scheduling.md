# 09 · Scheduling, cancellation, shutdown

## Flow

1. Validate request against capabilities.
2. Preprocess images (max `--preprocess-concurrency` at once). Outside the queue.
3. Enqueue the *ready* request. Bounded FIFO (`--queue-size`).
4. Single worker sends to the adapter.

- Slow image servers never block the model.

## Cancellation

- Client gone while queued -> request skipped.
- Client gone during inference -> request finishes, result dropped, engine keeps running.

## Crash

- Engine exits -> current request `runtime_crashed`.
- Queued requests rejected.
- `self` exits non-zero. No restart loop.

## Shutdown (Ctrl+C / SIGTERM)

1. Stop accepting requests.
2. Drop queued work (`shutting_down`).
3. Close engine stdin, wait.
4. Fallback SIGTERM, then SIGKILL.

## Acceptance criteria

- Must: queue full -> 429 immediately (`internal/decision` tests).
- Must: cancelled queued request never reaches the adapter.
- Must: cancelled in-flight request doesn't break the next one.
- Must: shutdown leaves no engine process (manual: "engine reaped").
- Must: race detector clean (`just test-race`).
