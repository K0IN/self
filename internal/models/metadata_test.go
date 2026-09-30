package models

import (
	"os"
	"path/filepath"
	"testing"

	"ai-server/internal/registry"
)

func TestRegistryMetadataCacheRoundTrip(t *testing.T) {
	reg, err := registry.Parse([]byte(`version: 1
models:
  demo:1b:
    description: demo
    type: decision
    default: 4bit
    info:
      context_length: 128
      max_options: 2
    4bit:
      adapter: ggmlc-laya
      repo: someone/demo
      files:
        - {file: demo.gguf, size: 4, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := SaveRegistry(root, reg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, ".registry.json"),
		filepath.Join(root, "demo", "1b", "metadata.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("metadata file %s: %v", path, err)
		}
	}
	cached, err := LoadRegistryCache(root)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := cached.Models["demo:1b"]
	if !ok || model.Description != "demo" || model.Variants["4bit"].Files[0].Name != "demo.gguf" {
		t.Fatalf("cached registry = %+v", cached)
	}
}
