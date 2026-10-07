package app

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"ai-server/internal/adapters"
	"ai-server/internal/audio"
	"ai-server/internal/benchmark"
	"ai-server/internal/config"
	"ai-server/internal/embedding"
	"ai-server/internal/image"
	"ai-server/internal/models"
	"ai-server/internal/registry"
	"ai-server/internal/runtime"
	"ai-server/internal/text"
)

func benchmarkOther(ctx context.Context, cfg config.Serve, t target, files models.Files, reported []benchmark.File, engine runtime.Engine, hw benchmark.Hardware, opt BenchmarkOptions, out io.Writer) (string, error) {
	started := time.Now()
	baseMiB, haveGPUMemory := benchmark.GPUMemoryMiB(ctx)
	var (
		results []benchmark.Result
		err     error
		unit    string
	)
	switch t.res.Model.Type {
	case registry.TypeText:
		results, unit, err = benchmarkText(ctx, cfg, t, files, engine, opt, out)
	case registry.TypeEmbedding:
		results, unit, err = benchmarkEmbedding(ctx, cfg, t, files, engine, opt, out)
	case registry.TypeAudio:
		results, unit, err = benchmarkAudio(ctx, cfg, t, files, engine, opt, out)
	case registry.TypeImage:
		results, unit, err = benchmarkImage(ctx, cfg, t, files, engine, opt, out)
	default:
		return "", fmt.Errorf("unsupported benchmark model type %q", t.res.Model.Type)
	}
	if err != nil {
		return "", err
	}
	gpuMiB := 0
	if used, ok := benchmark.GPUMemoryMiB(ctx); ok && haveGPUMemory {
		gpuMiB = max(used-baseMiB, 0)
	}
	gpuUsed := benchmark.GPUUsed(cfg.Device, hw, gpuMiB, t.res.Variant.Size())
	if strings.HasPrefix(cfg.Device, "cuda") && !gpuUsed {
		return "", fmt.Errorf("CUDA was requested but the benchmark used the CPU; check the CUDA runtime and engine logs")
	}
	commit, modified := buildCommit()
	for i := range results {
		if results[i].ThroughputUnit == "" {
			results[i].ThroughputUnit = unit
		}
	}
	report := benchmark.Report{
		Schema: benchmark.SchemaVersion, CreatedAt: time.Now().UTC().Truncate(time.Second),
		Benchmark: benchmark.Info{Version: benchmark.Version, Modality: string(t.res.Model.Type), Iterations: opt.Iterations, Warmup: opt.Warmup, SelfCommit: commit, SelfModified: modified},
		Model:     benchmark.Model{ID: t.res.ID(), Quant: t.res.Variant.Quant, Adapter: t.res.Variant.Adapter, Files: reported, Settings: map[string]any(t.settings)},
		Hardware:  hw, Device: benchmark.Device{Requested: cfg.Device, Engine: engineName(t.res.Model.Type, engine), GPUMemoryMiB: gpuMiB, GPUUsed: gpuUsed},
		LoadSeconds: math.Round(time.Since(started).Seconds()*100) / 100,
		Checks:      benchmark.Checks{}, Results: results,
	}
	if err := benchmark.Validate(report); err != nil {
		return "", fmt.Errorf("the report is inconsistent: %w", err)
	}
	path := benchmark.OutputPath(opt.Out, report)
	if err := benchmark.Write(path, report); err != nil {
		return "", err
	}
	printShareInstructions(out, path, report)
	return path, nil
}

func engineName(_ registry.ModelType, engine runtime.Engine) string { return engine.Path }

func benchmarkText(ctx context.Context, cfg config.Serve, t target, files models.Files, engine runtime.Engine, opt BenchmarkOptions, out io.Writer) ([]benchmark.Result, string, error) {
	entry, err := adapters.Text(t.res.Variant.Adapter)
	if err != nil {
		return nil, "", err
	}
	adapter := entry.New()
	settings := benchmarkRuntimeSettings(t.settings, cfg.Device)
	if err := adapter.Start(ctx, text.RuntimeConfig{ModelID: t.res.ID(), Files: text.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj]}, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: benchmarkLog(cfg), Settings: settings}); err != nil {
		return nil, "", err
	}
	defer closeAdapter(ctx, adapter.Close)
	scenarios := []benchmark.WorkScenario{{Name: "short-chat", Requests: opt.Iterations}, {Name: "long-chat", Requests: max(3, opt.Iterations/4)}}
	texts := []string{"Explain why a duplicated payment should be refunded in one sentence.", "Write a concise support reply for a delayed delivery."}
	index := 0
	results, err := benchmark.RunOperations(ctx, func(ctx context.Context) (benchmark.OperationResult, error) {
		prompt := texts[index%len(texts)]
		index++
		resp, err := adapter.Chat(ctx, text.Request{Messages: []text.Message{{Role: text.RoleUser, Parts: []text.Part{{Text: prompt}}}}, MaxTokens: 64}, nil)
		return benchmark.OperationResult{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens, Work: float64(resp.Usage.CompletionTokens), Unit: "output_tokens_per_sec"}, err
	}, scenarios, opt.Warmup, benchmarkProgress(out))
	return results, "output_tokens_per_sec", err
}

func benchmarkEmbedding(ctx context.Context, cfg config.Serve, t target, files models.Files, engine runtime.Engine, opt BenchmarkOptions, out io.Writer) ([]benchmark.Result, string, error) {
	entry, err := adapters.Embedding(t.res.Variant.Adapter)
	if err != nil {
		return nil, "", err
	}
	adapter := entry.New()
	settings := benchmarkRuntimeSettings(t.settings, cfg.Device)
	if err := adapter.Start(ctx, embedding.RuntimeConfig{ModelID: t.res.ID(), Files: embedding.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj]}, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: benchmarkLog(cfg), Settings: settings}); err != nil {
		return nil, "", err
	}
	defer closeAdapter(ctx, adapter.Close)
	results, err := benchmark.RunOperations(ctx, func(ctx context.Context) (benchmark.OperationResult, error) {
		input := "A compact sentence for measuring embedding throughput across repeated requests."
		_, tokens, err := adapter.Embed(ctx, embedding.Input{Text: input})
		return benchmark.OperationResult{InputTokens: tokens, Work: float64(tokens), Unit: "input_tokens_per_sec"}, err
	}, []benchmark.WorkScenario{{Name: "embedding", Requests: opt.Iterations}}, opt.Warmup, benchmarkProgress(out))
	return results, "input_tokens_per_sec", err
}

func benchmarkAudio(ctx context.Context, cfg config.Serve, t target, files models.Files, engine runtime.Engine, opt BenchmarkOptions, out io.Writer) ([]benchmark.Result, string, error) {
	entry, err := adapters.Audio(t.res.Variant.Adapter)
	if err != nil {
		return nil, "", err
	}
	adapter := entry.New()
	if err := adapter.Start(ctx, audio.RuntimeConfig{ModelID: t.res.ID(), Files: audio.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj], Voice: files[registry.RoleVoice]}, Instructions: t.res.Model.Capabilities.Has(registry.CapInstructions), Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: benchmarkLog(cfg), Settings: t.settings}); err != nil {
		return nil, "", err
	}
	defer closeAdapter(ctx, adapter.Close)
	results, err := benchmark.RunOperations(ctx, func(ctx context.Context) (benchmark.OperationResult, error) {
		resp, err := adapter.Synthesize(ctx, audio.Request{Input: "This is a short benchmark sentence for speech synthesis."})
		seconds := 0.0
		if resp.SampleRate > 0 && len(resp.WAV) > 44 {
			seconds = float64(len(resp.WAV)-44) / float64(resp.SampleRate*2)
		}
		return benchmark.OperationResult{Work: seconds, Unit: "audio_seconds_per_sec"}, err
	}, []benchmark.WorkScenario{{Name: "speech", Requests: opt.Iterations}}, opt.Warmup, benchmarkProgress(out))
	return results, "audio_seconds_per_sec", err
}

func benchmarkImage(ctx context.Context, cfg config.Serve, t target, files models.Files, engine runtime.Engine, opt BenchmarkOptions, out io.Writer) ([]benchmark.Result, string, error) {
	entry, err := adapters.Image(t.res.Variant.Adapter)
	if err != nil {
		return nil, "", err
	}
	adapter := entry.New()
	if err := adapter.Start(ctx, image.RuntimeConfig{ModelID: t.res.ID(), Files: image.ModelFiles{Model: files[registry.RoleModel], VAE: files[registry.RoleVAE], TextEncoder: files[registry.RoleTextEncoder], MMProj: files[registry.RoleMMProj]}, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: benchmarkLog(cfg), Settings: t.settings}); err != nil {
		return nil, "", err
	}
	defer closeAdapter(ctx, adapter.Close)
	const pixels = 256 * 256
	results, err := benchmark.RunOperations(ctx, func(ctx context.Context) (benchmark.OperationResult, error) {
		_, err := adapter.Generate(ctx, image.GenerateRequest{Prompt: "A red geometric apple on a white background", N: 1, Width: 256, Height: 256, OutputFormat: "png"})
		return benchmark.OperationResult{Work: pixels, Unit: "pixels_per_sec"}, err
	}, []benchmark.WorkScenario{{Name: "generation-256", Requests: opt.Iterations}}, opt.Warmup, benchmarkProgress(out))
	return results, "pixels_per_sec", err
}

func benchmarkLog(cfg config.Serve) io.Writer {
	if cfg.Verbose {
		return os.Stderr
	}
	return nil
}

func benchmarkRuntimeSettings(source map[string]any, device string) map[string]any {
	settings := make(map[string]any, len(source)+1)
	for key, value := range source {
		settings[key] = value
	}
	if device != "cpu" {
		settings["gpu_layers"] = int64(-1)
	}
	return settings
}

func closeAdapter(ctx context.Context, closeFn func(context.Context) error) {
	closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = closeFn(closeCtx)
}

func benchmarkProgress(out io.Writer) func(benchmark.Result) {
	return func(r benchmark.Result) {
		fmt.Fprintf(out, "  %-18s %10.1f %s\n", r.Scenario, r.Throughput, r.ThroughputUnit)
	}
}
