# Benchmarks

Benchmark reports make model speed comparable across real machines. The public
results are loaded at build time from one JSON file per run in the repository's [`benchmarks/` folder](https://github.com/K0IN/self/tree/main/benchmarks). Model cards use that generated data directly; the site keeps the same per-run layout and uses a small manifest to discover the files.

`self benchmark` supports every registry model type. It uses fixed scenarios
for decision models, chat prompts for text models, repeated sentences for
embeddings, a short phrase for speech synthesis, and a fixed 256x256 prompt
for image generation. Each report records the native throughput unit for its
modality, such as output tokens per second, audio seconds per second, or
pixels per second.

## Run a benchmark

Build or install `self`, then run the benchmark for the exact model and
quantization you want to measure:

```bash
self benchmark kev:0.5b --device auto --iterations 50 --out benchmarks/
```

## Copy and run with Docker

These commands check out the repository, run one GPU-first benchmark inside the
NVIDIA container, and leave the report in `benchmarks/` ready for a pull request:

```bash
git clone https://github.com/K0IN/self.git
cd self
docker run --rm --gpus all \
	-v "$PWD:/src" \
	-v "$HOME/.ai-server/models:/models" \
	-w /src \
	ghcr.io/k0in/self:cuda-12 \
	benchmark qwen3.5:9b --device cuda --iterations 50 --warmup 5 --out /src/benchmarks/
```

The `cuda-12` image matches the bundled CUDA 12 llama runtime. Keep
`--gpus all`; without it the container cannot see the NVIDIA device.

Before opening the pull request, inspect the report. It must contain
`"gpu_used": true`; otherwise the runtime did not execute on the GPU and the
report is a CPU baseline:

```bash
grep -E '"requested"|"engine"|"gpu_memory_mib"|"gpu_used"' benchmarks/*.json
git add benchmarks/*.json
git checkout -b benchmark-qwen3-5b-gpu
git commit -m "Add qwen3.5 GPU benchmark"
git push -u origin benchmark-qwen3-5b-gpu
```

Open the repository on GitHub and create a pull request from that branch. The
report filename and contents are checked by the benchmark tests.

On NVIDIA GPUs whose compute capability has a matching Laya CUDA runtime,
`--device auto` selects the GPU backend by default. The pinned Laya v0.9.6
runtime has CUDA builds through `sm89`; newer GPUs such as RTX 5090 (`sm120`)
need a compatible runtime release or a working Vulkan device exposed to the
container. The runtime setup checks for a visible Vulkan device before
installing that fallback. An unavailable GPU backend is an execution error,
not a CPU result filed as a GPU benchmark.

The normal `self serve` path uses the same GPU setting: text and embedding
models offload all layers by default on non-CPU devices. Set
`--set gpu_layers=0` or another explicit layer count when you need a different
offload policy.

Run on an otherwise idle machine. The command downloads the registry-pinned
files, verifies every SHA-256 hash, runs the fixed scenarios, records the CPU,
GPU, memory and driver, and writes a report. A GPU run is filed under the GPU;
if the model silently falls back to CPU, the report is filed under the CPU.
Use `--gpu "GPU name"` when the GPU cannot be detected automatically.

## Submit a report

The command prints a prefilled GitHub link. Open it and choose **Propose
changes**, or upload the JSON file to the repository's `benchmarks/` directory.
The pull request checks the filename, schema, model file hashes, hardware,
results, and benchmark metadata before it can be merged.

Reports must be produced from the checked-out `self` source, and each report
includes:

- `benchmark.version`, the version of the fixed scenarios and measurements.
- `benchmark.self_commit`, the source commit used to build the runner.
- `benchmark.modality`, one of `text`, `embedding`, `audio`, `image`, or `decision`.
- The exact model file hashes, quantization, adapter, device, hardware, and results.

## Read and update results

Model pages show the available benchmark runs by default. The hardware
selector contains only hardware with actual benchmark data. Detected GPUs are
listed separately until a run really uses them, so CPU timings can never appear
as GPU results. When several comparable runs exist, the model card shows the
newest one; older JSON reports remain in the repository for history. Compare
reports only when their
benchmark version, model file hashes, quantization, and modality match.

When scenarios or measurement semantics change, bump the benchmark version in
the benchmark package and document the change. Do not edit old reports to make
them look current; submit a new run. New scenarios should emit the same report
contract and use modality-specific names and units that are clear in the model
page.
