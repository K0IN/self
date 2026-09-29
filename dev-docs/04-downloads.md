# 04 · Model store and downloads

## Store layout

```
~/.ai-server/models/<name>/<tag>/<quant>/<file>
e.g. ~/.ai-server/models/kev/4b/4bit/kev_4b_ud_q4_k_m.gguf
```

- Human readable. No hashes in paths.
- Partial downloads: `<file>.part`. Never counted as installed.
- Lock file: `<file>.lock` (flock on Unix, LockFileEx on Windows).

## Download flow

1. Take the lock. Another process downloading -> clear error.
2. HEAD request: read `X-Linked-Size` / `X-Linked-Etag` (Hugging Face).
3. Compare with the registry pin. Different -> refuse ("upstream file changed").
4. GET with `Range` if a `.part` exists (resume).
5. Stream to `.part`, show progress.
6. Verify size + sha256 against the pin.
7. Rename `.part` -> final file. Mismatch -> delete, never install.

## Retries

- Up to 8 retries. Backoff 2s, doubling, max 30s.
- Retried: connection resets, TLS errors (`bad record MAC`), 5xx, stalls (60s no data).
- Not retried: 401/403 (hint: `HF_TOKEN`), 404.
- Each retry resumes from bytes on disk.

## Installed check

- File exists, is regular, size > 0, size == pinned size.
- sha256 is checked at download time only (hashing GBs on each start is too slow).

## Progress output

```
Downloading kev_4b_ud_q4_k_m.gguf
931 MiB / 3.92 GiB
████████░░░░░░░░ 23%
10.67 MiB/s   ETA 4m49s
Retrying in 2s (attempt 1/8), resuming from 777 MiB...
```

## Acceptance criteria

- Must: interrupted download resumes (tests: resume, server ignores Range, interrupted keeps part).
- Must: TLS/stall errors are retried and resume (tests: retries, stall).
- Must: wrong bytes vs pin -> file deleted, not installed (`TestEnsureRejectsPinMismatch`).
- Must: server advertises different file -> refused before body download (`TestEnsureRefusesChangedUpstream`).
- Must: wrong-size installed file is re-downloaded (`TestInstalledChecksPinnedSize`).
- Must: two processes can't write the same file (`TestDownloadLockPreventsConcurrentWriters`).
- Manual: installed kev:0.5b, kev:4b, decider:2b-vision match registry sha256 (verified).
