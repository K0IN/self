// Package clef wires the native Clef SELFIPC engine into self.
package clef

import (
	"fmt"

	"ai-server/internal/adapters/selfipc"
	"ai-server/internal/decision"
	"ai-server/internal/ggufmeta"
	"ai-server/internal/settings"
)

const Engine = "clef"

var Settings = settings.Schema{
	{Name: "context_size", Flag: "--ctx", Kind: settings.Int, Min: 512, Max: 262144},
	{Name: "threads", Flag: "--threads", Kind: settings.Int, Min: 1, Max: 1024},
	{Name: "gpu_layers", Flag: "--gpu-layers", Kind: settings.Int, Min: -1, Max: 10000},
}

var Spec = selfipc.Spec{
	Args: func(cfg decision.RuntimeConfig) []string {
		args := []string{"--model", cfg.Files.Model, "--head", cfg.Files.Head, "--device", cfg.Device}
		if cfg.Files.MMProj != "" {
			args = append(args, "--mmproj", cfg.Files.MMProj)
		}
		return append(args, Settings.Args(cfg.Settings)...)
	},
	Check: func(cfg decision.RuntimeConfig) error {
		model, err := ggufmeta.ReadFile(cfg.Files.Model)
		if err != nil {
			return fmt.Errorf("cannot read Clef GGUF: %w", err)
		}
		if ggufmeta.String(model, "general.architecture") == "" {
			return fmt.Errorf("Clef GGUF has no architecture metadata")
		}
		if cfg.Files.Head == "" {
			return fmt.Errorf("Clef joint head is required")
		}
		return nil
	},
}
