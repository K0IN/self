# 04 · Model store and downloads

## Store layout

```
~/.ai-server/models/<name>/<tag>/<quant>/<file>
e.g. ~/.ai-server/models/kev/4b/q4/kev_4b_ud_q4_k_m.gguf
```

- Human readable. No hashes in paths.
- Partial downloads: `<file>.part`. Never counted as installed.
- Lock file: `<file>.lock` (non-blocking flock on Unix, LockFileEx on Windows). It
  is removed when the download ends.
- Registry cache next to the models: `<models-dir>/.registry.yml` and
  `<name>/<tag>/metadata.json` (see 03).
- `HF_ENDPOINT` replaces `https://huggingface.co`; `HF_TOKEN` is sent as a
  bearer token; the usual proxy environment variables are honored.

## Download flow

1. Take the lock. Another process downloading -> clear error, no waiting.
2. HEAD request (no redirects): read `X-Linked-Size` / `X-Linked-Etag` (Hugging Face). Best effort.
3. Compare with the registry pin. Different -> refuse ("upstream file changed"), before any body bytes.
4. GET with `Range` if a `.part` exists (resume).
5. Stream to `.part`, show progress.
6. Verify size + sha256 against the pin.
7. Rename `.part` -> final file. Mismatch -> delete, never install.

## Retries

- Up to 8 retries. Backoff 2s, doubling, max 30s.
- Retried: connection resets, TLS errors (`bad record MAC`), 5xx and other unexpected statuses, stalls (60s no data), wrong `Content-Range`.
- Not retried: 401/403 (hint: `HF_TOKEN`), 404, cancellation (Ctrl+C keeps the `.part`).
- Each retry resumes from bytes on disk. A `.part` that fails verification is deleted.

## Installed check

- File exists, is regular, size > 0, size == pinned size.
- sha256 is checked at download time only (hashing GBs on each start is too slow).

## Progress output

```
Downloading kev_4b_ud_q4_k_m.gguf

931 MiB / 3.92 GiB
████████░░░░░░░░ 23%
10.67 MiB/s   ETA 4m49s
Connection problem: <error>
Retrying in 2s (attempt 1/8), resuming from 777 MiB...
```

- A resumed download starts with `Resuming <file> at <size>`.
- On a TTY the three lines are redrawn in place. Without a TTY, one line is
  printed every 5 seconds.

## Acceptance criteria

- Must: interrupted download resumes (tests: resume, server ignores Range, interrupted keeps part).
- Must: TLS/stall errors are retried and resume (tests: retries, stall).
- Must: wrong bytes vs pin -> file deleted, not installed (`TestEnsureRejectsPinMismatch`).
- Must: server advertises different file -> refused before body download (`TestEnsureRefusesChangedUpstream`).
- Must: wrong-size installed file is re-downloaded (`TestInstalledChecksPinnedSize`).
- Must: two processes can't write the same file (`TestDownloadLockPreventsConcurrentWriters`).
- Must: `self rm` removes finished and partial files of one or all quants (`TestStoreRemove`).
- Manual (verified): the ten GGUF files in a local model store (default quants of the nine registry models) all match the registry sha256.
