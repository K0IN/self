// Package models manages the local model directory and downloads model
// files from Hugging Face.
package models

import (
	"os"
	"path/filepath"

	"ai-server/internal/registry"
)

// Store is the root models directory, laid out for humans:
//
//	<root>/<name>/<tag>/<quant>/<file>
//	e.g. ~/.ai-server/models/decider-vision/2b/q4/decider-2b-vision.Q4_K_M.gguf
type Store struct {
	Root string
}

// Dir returns the directory of a resolved variant.
func (s Store) Dir(r registry.Resolved) string {
	tag := r.Tag()
	if tag == "" {
		tag = "latest"
	}
	return filepath.Join(s.Root, r.Name(), tag, r.Variant.Quant)
}

// Path returns the final path of a model file.
func (s Store) Path(r registry.Resolved, f registry.File) string {
	return filepath.Join(s.Dir(r), filepath.FromSlash(f.Name))
}

// Installed reports whether a completed file exists. Partial downloads live
// at <path>.part and never count as installed. When the registry pins a
// size, a file of a different size (e.g. an older upstream revision) is not
// installed either. The sha256 is checked once at download time, not on
// every start (hashing multi-GB files would slow startup).
func (s Store) Installed(r registry.Resolved, f registry.File) bool {
	st, err := os.Stat(s.Path(r, f))
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 {
		return false
	}
	return f.Size <= 0 || st.Size() == f.Size
}
