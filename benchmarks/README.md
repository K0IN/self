# Benchmarks

Speed reports written by `self benchmark <model>`, one JSON file per run and
machine. Add yours, so others can see how a model runs on hardware like theirs.

## Add a report

```bash
self benchmark kev:0.5b
```

The command downloads the model if needed, checks the file hashes, times a fixed
set of requests, and writes `<model>-<quant>-<rig>-<time>.json`. It prints a
GitHub link with your report already filled in: open it, sign in and choose
"Propose changes". GitHub forks the repository and opens the pull request for
you. You can also drop the file into this folder with "Add file, Upload files".

Run it on an otherwise idle machine, and use `--iterations` for steadier
numbers. Add `--gpu "<name>"` when your GPU is not an NVIDIA one, so the report
says which GPU it ran on.

## What a report holds

- the model, quant and adapter, and the sha256 of every model file that was run
- CPU, threads, RAM, GPU and driver, and whether the model was really loaded
  onto the GPU (`device.gpu_used`)
- the benchmark version (`benchmark.version`), which changes whenever the
  requests change, and the commit `self` was built from
- whether the model answered the `self check` probes correctly
- per scenario: latency (min, mean, p50, p95, max) as seen by the caller and as
  reported by the engine, requests per second and input tokens per second

Reports contain no host name, user name or file paths. Only compare reports with
the same `benchmark.version`. `go test ./internal/benchmark` checks every file in
this folder.
