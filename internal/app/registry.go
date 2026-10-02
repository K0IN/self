package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"ai-server/internal/models"
	"ai-server/internal/registry"
)

// LoadRegistry loads the registry from source: an http(s) URL (default: the
// published GitHub Pages document) or a local file. A URL is cached in
// modelsDir; if the fetch fails or the document is invalid, the last cached
// copy is used so already downloaded models keep working offline. A local
// file is used as-is: no cache and no fallback, so it never replaces the
// cached published registry.
func LoadRegistry(source, modelsDir string) (*registry.Registry, error) {
	if !isURL(source) {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("load registry: %w", err)
		}
		reg, err := registry.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parse registry %s: %w", source, err)
		}
		return reg, nil
	}
	return loadRegistryCached(&http.Client{Timeout: 30 * time.Second}, source, modelsDir)
}

func isURL(source string) bool {
	return strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://")
}

func loadRegistryCached(client *http.Client, source, modelsDir string) (*registry.Registry, error) {
	reg, raw, err := registry.Fetch(client, source)
	if err == nil {
		// A read-only models directory must not block serving.
		if cacheErr := models.SaveRegistry(modelsDir, reg, raw); cacheErr != nil {
			slog.Warn("registry cache not updated", "err", cacheErr)
		}
		return reg, nil
	}
	cached, cacheErr := models.LoadRegistryCache(modelsDir)
	if cacheErr != nil {
		return nil, fmt.Errorf("load registry: %w (offline cache unavailable: %v)", err, cacheErr)
	}
	slog.Warn("registry unavailable, using cached copy", "err", err)
	return cached, nil
}
