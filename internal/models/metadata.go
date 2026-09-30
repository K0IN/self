package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-server/internal/registry"
)

const registryCacheName = ".registry.yml"

// SaveRegistry caches the complete registry and one metadata file per model.
// The cache is written after a successful remote fetch so future starts can be
// fully offline for models that have already been discovered.
func SaveRegistry(root string, reg *registry.Registry, source []byte) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, registryCacheName), source, 0o644); err != nil {
		return fmt.Errorf("write registry cache: %w", err)
	}
	for id, model := range reg.Models {
		name, tag, _ := strings.Cut(id, ":")
		if tag == "" {
			tag = "latest"
		}
		dir := filepath.Join(root, name, tag)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create metadata directory for %s: %w", id, err)
		}
		if err := writeJSON(filepath.Join(dir, "metadata.json"), model); err != nil {
			return fmt.Errorf("write metadata for %s: %w", id, err)
		}
	}
	return nil
}

// LoadRegistryCache loads the last successfully fetched registry.
func LoadRegistryCache(root string) (*registry.Registry, error) {
	reg, err := registry.LoadFile(filepath.Join(root, registryCacheName))
	if err != nil {
		return nil, fmt.Errorf("read registry cache: %w", err)
	}
	return reg, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
