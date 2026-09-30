// Package models manages the local model directory and downloads model
// files from Hugging Face.
package models

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"ai-server/internal/errs"
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

// pathSegment is what a name, tag or quant must look like before it is used
// to build a path that gets deleted.
var pathSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Remove deletes the downloaded quants of model id ("name:tag"), including
// partial downloads, and returns the quants removed. An empty quant removes
// all of them. It does not consult the registry, so models that were dropped
// from it can still be removed.
func (s Store) Remove(id, quant string) ([]string, error) {
	name, tag, _ := strings.Cut(id, ":")
	if tag == "" {
		tag = "latest"
	}
	segments := []string{name, tag}
	ref := id
	if quant != "" {
		segments = append(segments, quant)
		ref += "@" + quant
	}
	for _, seg := range segments {
		if !pathSegment.MatchString(seg) {
			return nil, errs.New(errs.InvalidRequest, "invalid model reference %q", ref)
		}
	}
	tagDir := filepath.Join(s.Root, name, tag)
	entries, err := os.ReadDir(tagDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		if !e.IsDir() || (quant != "" && e.Name() != quant) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(tagDir, e.Name())); err != nil {
			return removed, err
		}
		removed = append(removed, e.Name())
	}
	// os.Remove only succeeds on empty directories, which prunes leftovers.
	_ = os.Remove(tagDir)
	_ = os.Remove(filepath.Join(s.Root, name))
	return removed, nil
}
