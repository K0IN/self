// Package customdecider wires the ggmlc-custom-decider engine
// (engines/ggmlc-custom-decider) into the generic SELFIPC1 adapter.
//
// The engine runs Decider checkpoints (Mapika/decider family: Qwen3.5 text or
// vision models with a lettered one-pass readout) from llama.cpp GGUFs, with
// an optional mmproj file for image input.
package customdecider

import (
	"fmt"

	"ai-server/internal/adapters/selfipc"
	"ai-server/internal/decision"
	"ai-server/internal/ggufmeta"
	"ai-server/internal/settings"
)

// Engine is the bundled executable name.
const Engine = "ggmlc-custom-decider"

// Settings are the engine parameters (registry `settings:` / `--set`).
// Keep in sync with the flags in engines/ggmlc-custom-decider/main.cpp.
var Settings = settings.Schema{
	{Name: "context_size", Flag: "--ctx", Kind: settings.Int, Min: 512, Max: 262144,
		Help: "llama.cpp context size in tokens (prompt + image tokens); default 8192"},
	{Name: "threads", Flag: "--threads", Kind: settings.Int, Min: 1, Max: 1024,
		Help: "CPU threads; default: llama.cpp default"},
	{Name: "temperature", Flag: "--temperature", Kind: settings.Float, Min: 0.01, Max: 100,
		Help: "softmax temperature on the letter logits; default 1.0"},
	{Name: "gpu_layers", Flag: "--gpu-layers", Kind: settings.Int, Min: -1, Max: 10000,
		Help: "layers offloaded to the GPU; -1 = all (default), 0 = CPU"},
	{Name: "flash_attn", Flag: "--flash-attn", Kind: settings.String, Enum: []string{"auto", "on", "off"},
		Help: "flash attention; default auto"},
	{Name: "image_min_tokens", Flag: "--image-min-tokens", Kind: settings.Int, Min: 1, Max: 65536,
		Help: "minimum tokens per image (dynamic-resolution vision models)"},
	{Name: "image_max_tokens", Flag: "--image-max-tokens", Kind: settings.Int, Min: 1, Max: 65536,
		Help: "maximum tokens per image (dynamic-resolution vision models)"},
}

// Spec is the selfipc spec for the engine.
var Spec = selfipc.Spec{
	Args: func(cfg decision.RuntimeConfig) []string {
		args := []string{"--model", cfg.Files.Model, "--device", cfg.Device}
		if cfg.Files.MMProj != "" {
			args = append(args, "--mmproj", cfg.Files.MMProj)
		}
		return append(args, Settings.Args(gpuDefaults(cfg.Device, cfg.Settings))...)
	},
	Check: func(cfg decision.RuntimeConfig) error {
		md, err := ggufmeta.ReadFile(cfg.Files.Model)
		if err != nil {
			return fmt.Errorf("cannot read GGUF metadata: %w", err)
		}
		return CheckModel(md, cfg.Files.MMProj)
	},
}

func gpuDefaults(device string, source settings.Values) settings.Values {
	if device == "cpu" {
		return source
	}
	values := settings.Values{}
	for key, value := range source {
		values[key] = value
	}
	if _, ok := values["gpu_layers"]; !ok {
		values["gpu_layers"] = int64(-1)
	}
	return values
}

// CheckModel verifies that a GGUF is a llama.cpp checkpoint the engine can
// load (not a ggmlc-compiled graph, which belongs to ggmlc-laya).
func CheckModel(md ggufmeta.Metadata, mmproj string) error {
	arch := ggufmeta.String(md, "general.architecture")
	switch {
	case arch == "":
		return fmt.Errorf("the GGUF has no general.architecture")
	case arch == "ggmlc" || ggufmeta.Has(md, "ggmlc.graph_spec"):
		return fmt.Errorf("the GGUF is a ggmlc-compiled graph; use adapter ggmlc-laya")
	case arch == "clip":
		return fmt.Errorf("the model file is a multimodal projector; list it with role: mmproj")
	case !ggufmeta.Has(md, "tokenizer.ggml.tokens"):
		return fmt.Errorf("the GGUF has no embedded tokenizer")
	}
	if mmproj != "" {
		pm, err := ggufmeta.ReadFile(mmproj)
		if err != nil {
			return fmt.Errorf("cannot read mmproj metadata: %w", err)
		}
		if ggufmeta.String(pm, "general.architecture") != "clip" {
			return fmt.Errorf("the mmproj file is not a clip/mmproj GGUF")
		}
	}
	return nil
}
