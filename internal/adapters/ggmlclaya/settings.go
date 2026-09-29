package ggmlclaya

import (
	"strings"

	"ai-server/internal/decision"
	"ai-server/internal/settings"
)

// Settings are the `laya daemon` parameters (registry `settings:` / `--set`).
// Upstream flags: laya daemon <gguf> [--device] [--threads N] [--cuda-graph].
// Context length and option limits are compiled into ggmlc GGUFs and can't
// be changed at runtime.
var Settings = settings.Schema{
	{Name: "threads", Flag: "--threads", Kind: settings.Int, Min: 1, Max: 1024,
		Help: "CPU workers; upstream default 4"},
	{Name: "cuda_graph", Flag: "--cuda-graph", Kind: settings.Bool,
		Help: "capture a CUDA graph for the live shape; default on for auto/cuda devices"},
}

// laArgs renders settings, defaulting cuda_graph to on for CUDA-capable
// device selectors (upstream default is off).
func laArgs(cfg decision.RuntimeConfig) []string {
	v := settings.Values{}
	for k, x := range cfg.Settings {
		v[k] = x
	}
	if _, set := v["cuda_graph"]; !set {
		v["cuda_graph"] = cfg.Device == "auto" || strings.HasPrefix(cfg.Device, "cuda")
	}
	return Settings.Args(v)
}
