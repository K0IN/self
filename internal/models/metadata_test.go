package models

import (
	"os"
	"path/filepath"
	"testing"

	"ai-server/internal/registry"
)

func TestRegistryMetadataCacheRoundTrip(t *testing.T) {
	source := []byte("version: 1\n" +
		"models:\n" +
		"  demo:1b:\n" +
		"    description: demo\n" +
		"    readme: readmes/demo/1b.md\n" +
		"    type: decision\n" +
		"    default: q4\n" +
		"    info:\n" +
		"      context_length: 128\n" +
		"      max_options: 2\n" +
		"    q4:\n" +
		"      adapter: ggmlc-laya\n" +
		"      repo: someone/demo\n" +
		"      files:\n" +
		"        - file: demo.gguf\n" +
		"          size: 4\n" +
		"          sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	reg, err := registry.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := SaveRegistry(root, reg, source); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, ".registry.yml"),
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
	if !ok || model.Description != "demo" || model.Variants["q4"].Files[0].Name != "demo.gguf" {
		t.Fatalf("cached registry = %+v", cached)
	}
}
