package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-server/internal/registry"
)

// TestBundledRegistry validates the embedded registry and that every model
// card it links exists next to it and is non-empty.
func TestBundledRegistry(t *testing.T) {
	reg, err := registry.Parse(Registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range reg.IDs() {
		m := reg.Models[id]
		b, err := os.ReadFile(filepath.FromSlash(m.Readme))
		if err != nil {
			t.Errorf("%s: readme %s: %v", id, m.Readme, err)
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(string(b)), "# ") {
			t.Errorf("%s: readme %s should start with a '# ' title", id, m.Readme)
		}
	}
}
