# 09 · Scheduling, cancellation, shutdown

## Flow

1. Validate request against capabilities (question types, option counts, image count; max 64 questions).
2. Preprocess images (max `--preprocess-concurrency` requests at once; the images of one request run one after another). Outside the queue.
3. Enqueue the *ready* request. Bounded FIFO (`--queue-size`). Full -> `queue_full` (429) at once.
4. Single worker sends to the adapter. `usage.queue_ms` is the time spent in the queue.

- Slow image servers never block the model.

## Cancellation

- Client gone while queued -> request skipped.
- Client gone during inference -> request finishes, result dropped, engine keeps running.

## Crash

- Engine exits (or a request gets no answer within 2 minutes and the engine is killed) -> current request `runtime_crashed`.
- Queued and later requests are rejected with the same error.
- `GET /health` reports runner `crashed` (503).
- `self` exits non-zero (`Error [runtime_crashed]`, last engine stderr lines attached). No restart loop.

## Shutdown (Ctrl+C / SIGTERM)

1. Runner state becomes `stopping` (`/health` 503). Stop accepting requests (10 s grace for in-flight handlers).
2. Drop queued work (`shutting_down`); wait for the running pass.
3. Close engine stdin, wait 5 s.
4. Fallback SIGTERM, then SIGKILL after 3 s.

Exit status is 0 after a clean shutdown.

## Acceptance criteria

- Must: queue full -> 429 immediately (`TestSchedulerQueueFull`, `TestQueueFull429`).
- Must: requests run in FIFO order on one runner (`TestSchedulerFIFO`, `TestSchedulerSingleRunnerConcurrentCallers`).
- Must: cancelled queued request never reaches the adapter (`TestSchedulerQueuedCancellationSkipsRun`).
- Must: cancelled in-flight request doesn't break the next one (`TestSchedulerRunningCancellationCompletes`).
- Must: a crash fails queued requests (`TestSchedulerCrashFailsQueued`).
- Must: shutdown leaves no engine process (manual, verified: no engine process after SIGINT; end-to-end test `lifecycle: graceful shutdown`).
- Must: race detector clean (`go test -race ./...`, verified).
