package registry

import (
	"fmt"
	"io"
	"net/http"
)

// PublishedURL is the registry document published on GitHub Pages.
const PublishedURL = "https://k0in.github.io/self/models.yml"

const maxDocumentBytes = 16 << 20

// Fetch downloads and parses a registry document. The raw bytes are returned
// so callers can cache exactly what was fetched.
func Fetch(client *http.Client, source string) (*Registry, []byte, error) {
	resp, err := client.Get(source)
	if err != nil {
		return nil, nil, fmt.Errorf("load registry %s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, fmt.Errorf("load registry %s: HTTP %s", source, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("read registry %s: %w", source, err)
	}
	reg, err := Parse(b)
	if err != nil {
		return nil, nil, fmt.Errorf("parse registry %s: %w", source, err)
	}
	return reg, b, nil
}
