package models_test

import (
	"os"
	"testing"

	"ai-server/internal/registry"
)

func TestSupportedModels(t *testing.T) {
	data, err := os.ReadFile("../../models/registry.yml")
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	parsed, err := registry.Parse(data)
	if err != nil {
		t.Fatalf("parse source registry: %v", err)
	}
	if len(parsed.Models) == 0 {
		t.Fatal("source model registry is empty")
	}

	for id, model := range parsed.Models {
		id, model := id, model
		t.Run(id, func(t *testing.T) {
			if model.Default == "" {
				t.Fatal("default quant is empty")
			}
			quant, ok := model.Variants[model.Default]
			if !ok {
				t.Fatalf("default quant %q is not defined", model.Default)
			}
			if quant.Adapter == "" {
				t.Fatal("default quant adapter is empty")
			}
			if len(quant.Files) == 0 {
				t.Fatal("default quant has no model files")
			}
			for _, file := range quant.Files {
				if file.Name == "" {
					t.Fatal("model file name is empty")
				}
				if file.Size <= 0 {
					t.Fatalf("model file %q has invalid size %d", file.Name, file.Size)
				}
				if file.SHA256 == "" {
					t.Fatalf("model file %q has no sha256 pin", file.Name)
				}
			}
		})
	}
}
