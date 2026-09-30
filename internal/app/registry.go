package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"ai-server/internal/models"
	"ai-server/internal/registry"
)

// LoadRegistry loads the registry from the published GitHub Pages document and
// caches it in modelsDir. If the fetch fails or the document is invalid, the
// last cached copy is used so already downloaded models keep working offline.
func LoadRegistry(modelsDir string) (*registry.Registry, error) {
	return loadRegistryCached(&http.Client{Timeout: 30 * time.Second}, registry.PublishedURL, modelsDir)
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
