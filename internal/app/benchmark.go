package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"ai-server/internal/benchmark"
	"ai-server/internal/config"
	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/imageutil"
	"ai-server/internal/models"
	"ai-server/internal/registry"
)

// BenchmarkOptions are the `self benchmark` settings on top of the serve flags.
type BenchmarkOptions struct {
	Iterations int    // timed requests per scenario
	Warmup     int    // untimed requests before each scenario
	Out        string // report file or directory; empty means the current directory
	GPU        string // GPU name to record when none can be detected (non-NVIDIA)
}

// Benchmark runs `self benchmark`: resolve, download, verify the file hashes,
// start the engine, time a fixed set of requests and write a report. It
// returns the report's path.
func Benchmark(ctx context.Context, cfg config.Serve, opt BenchmarkOptions, out io.Writer, isTTY bool) (string, error) {
	t, err := resolveTarget(cfg)
	if err != nil {
		return "", err
	}
	if t.res.Model.Type != registry.TypeDecision {
		return "", errs.New(errs.UnsupportedModel, "benchmark supports decision models only")
	}
	res, set := t.res, t.settings
	engine, err := t.locate(cfg)
	if err != nil {
		return "", err
	}
	t.describe(out, engine.Path)

	store := models.Store{Root: cfg.ModelsDir}
	files, err := download(ctx, cfg, res, out, isTTY)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(out)
	reported, err := hashModelFiles(ctx, store, res, out)
	if err != nil {
		return "", err
	}

	hw := benchmark.DetectHardware(ctx)
	if opt.GPU != "" {
		hw.GPUs = []benchmark.GPU{{Name: opt.GPU}}
	}
	fmt.Fprintf(out, "Hardware %s, %d threads, %.1f GiB RAM, GPU %s\n", hw.CPU, hw.CPUThreads, hw.RAMGiB, gpuNames(hw))

	baseMiB, haveGPUMemory := benchmark.GPUMemoryMiB(ctx)
	started := time.Now()
	de, err := startDecision(ctx, cfg, t, files, engine, nil)
	if err != nil {
		return "", err
	}
	defer de.close()
	adapter, info, caps := de.adapter, de.info, de.caps
	load := time.Since(started)
	fmt.Fprintf(out, "Loaded in %s (device %s)\n", load.Round(time.Millisecond), info.Device)

	gpuMiB := 0
	if used, ok := benchmark.GPUMemoryMiB(ctx); ok && haveGPUMemory {
		gpuMiB = max(used-baseMiB, 0)
	}
	if cfg.Device != "cpu" && len(hw.GPUs) == 0 {
		fmt.Fprintln(out, "Note     no NVIDIA GPU detected. If the model runs on another GPU, add --gpu \"<name>\" so the report says so.")
	}

	fmt.Fprintln(out, "\nChecking the answers")
	passed, total, err := runProbes(ctx, adapter, caps, func(r probeResult) {
		switch {
		case r.skipped:
			fmt.Fprintf(out, "  skip  %s\n", r.name)
		case r.err != nil:
			fmt.Fprintf(out, "  FAIL  %-28s %v\n", r.name, r.err)
		case !r.ok:
			fmt.Fprintf(out, "  FAIL  %-28s %s\n", r.name, r.detail)
		default:
			fmt.Fprintf(out, "  ok    %s\n", r.name)
		}
	})
	if err != nil {
		return "", err
	}
	if passed < total {
		fmt.Fprintf(out, "Warning  %d of %d probes failed: the timings are valid but the answers look wrong, see `self check`.\n", total-passed, total)
	}

	in := benchmark.Inputs{Caps: caps, Iterations: opt.Iterations}
	var contextSize int64
	if v, ok := set["context_size"].(int64); ok {
		contextSize = v
	}
	in.ContextTokens = benchmark.LongContextTokens(res.Model.Info.ContextLength, contextSize)
	if caps.Vision {
		img, err := imageutil.NewPreprocessor(imageutil.DefaultLimits()).Prepare(ctx, decision.ImageSource{URL: benchmark.ImageDataURI()}, *info.ImageInput)
		if err != nil {
			return "", err
		}
		in.Image = &img
	}
	scenarios, err := benchmark.Scenarios(in)
	if err != nil {
		return "", errs.New(errs.UnsupportedModel, "%v", err)
	}

	fmt.Fprintf(out, "\nBenchmark (%d warmup + %d timed requests per scenario, one at a time)\n", opt.Warmup, opt.Iterations)
	fmt.Fprintf(out, "  %-15s %6s %10s %10s %10s\n", "scenario", "tokens", "p50 ms", "p95 ms", "tokens/s")
	results, err := benchmark.Run(ctx, adapter, scenarios, opt.Warmup, func(r benchmark.Result) {
		fmt.Fprintf(out, "  %-15s %6d %10.1f %10.1f %10.1f\n", r.Scenario, r.InputTokens, r.Latency.P50, r.Latency.P95, r.InputTokensPerSec)
	})
	if err != nil {
		return "", err
	}
	if used, ok := benchmark.GPUMemoryMiB(ctx); ok && haveGPUMemory {
		gpuMiB = max(gpuMiB, used-baseMiB)
	}
	gpuUsed := benchmark.GPUUsed(cfg.Device, hw, gpuMiB, res.Variant.Size())
	if !gpuUsed && cfg.Device != "cpu" && len(hw.GPUs) > 0 {
		fmt.Fprintf(out, "\nWarning  the model added only %d MiB of GPU memory (%d MiB of weights): it ran on the CPU.\n         The report is filed as a CPU run. Fix the GPU runtime, or use --device cpu.\n", gpuMiB, res.Variant.Size()>>20)
	}

	commit, modified := buildCommit()
	report := benchmark.Report{
		Schema:    benchmark.SchemaVersion,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Benchmark: benchmark.Info{Version: benchmark.Version, Iterations: opt.Iterations, Warmup: opt.Warmup, SelfCommit: commit, SelfModified: modified},
		Model: benchmark.Model{
			ID: res.ID(), Quant: res.Variant.Quant, Adapter: res.Variant.Adapter,
			Files: reported, Settings: map[string]any(set),
		},
		Hardware:    hw,
		Device:      benchmark.Device{Requested: cfg.Device, Engine: info.Device, GPUMemoryMiB: gpuMiB, GPUUsed: gpuUsed},
		LoadSeconds: math.Round(load.Seconds()*100) / 100,
		Checks:      benchmark.Checks{ProbesPassed: passed, ProbesTotal: total},
		Results:     results,
	}
	if err := benchmark.Validate(report); err != nil {
		return "", errs.New(errs.Internal, "the report is inconsistent: %v", err)
	}
	path := benchmark.OutputPath(opt.Out, report)
	if err := benchmark.Write(path, report); err != nil {
		return "", err
	}
	printShareInstructions(out, path, report)
	return path, nil
}

// printShareInstructions tells the user how to hand the report in.
func printShareInstructions(out io.Writer, path string, r benchmark.Report) {
	fmt.Fprintf(out, "\nReport   %s\n", path)
	fmt.Fprintln(out, "\nShare it as a pull request on GitHub:")
	if link, ok := benchmark.NewFileURL(r); ok {
		fmt.Fprintf(out, "  1. Open this link, sign in to GitHub and choose \"Propose changes\" (your report is filled in):\n\n%s\n\n", link)
		fmt.Fprintf(out, "  2. Or upload the file by hand: %s\n", benchmark.UploadPageURL())
	} else {
		fmt.Fprintf(out, "  Upload the file at %s\n", benchmark.UploadPageURL())
	}
	fmt.Fprintln(out, "\nThe report holds the model, file hashes, CPU, GPU and RAM, and the timings. It has no host name, user name or file paths.")
}

func gpuNames(h benchmark.Hardware) string {
	if len(h.GPUs) == 0 {
		return "none detected"
	}
	names := make([]string, len(h.GPUs))
	for i, g := range h.GPUs {
		names[i] = g.Name
	}
	return strings.Join(names, ", ")
}

// hashModelFiles computes the sha256 of every model file and requires it to
// match the registry pin, so a report always describes the published weights.
func hashModelFiles(ctx context.Context, store models.Store, res registry.Resolved, out io.Writer) ([]benchmark.File, error) {
	files := make([]benchmark.File, 0, len(res.Variant.Files))
	for _, f := range res.Variant.Files {
		path := store.Path(res, f)
		fmt.Fprintf(out, "Verifying %s ... ", f.Name)
		sum, size, err := sha256File(ctx, path)
		if err != nil {
			fmt.Fprintln(out)
			return nil, err
		}
		if f.SHA256 != "" && sum != strings.ToLower(f.SHA256) {
			fmt.Fprintln(out, "MISMATCH")
			return nil, errs.New(errs.DownloadFailed, "%s does not match the registry pin (sha256 %s, want %s): run `self rm %s@%s` and try again", f.Name, sum, strings.ToLower(f.SHA256), res.ID(), res.Variant.Quant)
		}
		fmt.Fprintln(out, "ok")
		files = append(files, benchmark.File{Name: f.Name, Role: string(f.Role), Size: size, SHA256: sum})
	}
	return files, nil
}

func sha256File(ctx context.Context, path string) (sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err = io.Copy(h, &ctxReader{ctx: ctx, r: f})
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// ctxReader stops a long read when the context is cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// buildCommit is the git commit this binary was built from, when known.
func buildCommit() (commit string, modified bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
			if len(commit) > 12 {
				commit = commit[:12]
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return commit, modified
}
