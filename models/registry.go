// Package models provides access to the published model registry for tooling.
package models

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"ai-server/internal/registry"
)

const registryURL = "https://k0in.github.io/self/models.yml"

// Registry downloads and parses the current published model registry.
func Registry() (*registry.Registry, error) {
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(registryURL)
	if err != nil {
		return nil, fmt.Errorf("load registry %s: %w", registryURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("load registry %s: HTTP %s", registryURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read registry %s: %w", registryURL, err)
	}
	result, err := registry.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", registryURL, err)
	}
	return result, nil
}
