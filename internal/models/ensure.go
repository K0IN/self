package models

import (
	"context"

	"ai-server/internal/registry"
)

// Files are the local paths of a variant's files, by role.
type Files map[registry.FileRole]string

// Ensure downloads any missing files of r and returns local paths.
// newProgress is called once per file that needs downloading.
func Ensure(ctx context.Context, s Store, d *Downloader, r registry.Resolved, newProgress func() Progress) (Files, error) {
	out := Files{}
	for _, f := range r.Variant.Files {
		dest := s.Path(r, f)
		if !s.Installed(r, f) {
			var p Progress
			if newProgress != nil {
				p = newProgress()
			}
			pin := Pin{Size: f.Size, SHA256: f.SHA256}
			if err := d.DownloadPinned(ctx, d.FileURL(r.Variant.RepoOf(f), f.Name), dest, pin, p); err != nil {
				return out, err
			}
		}
		out[f.Role] = dest
	}
	return out, nil
}
