package models

import (
	"context"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/registry"
)

// Ensure downloads any missing files of r and returns local paths.
// newProgress is called once per file that needs downloading.
func Ensure(ctx context.Context, s Store, d *Downloader, r registry.Resolved, newProgress func() Progress) (decision.ModelFiles, error) {
	var out decision.ModelFiles
	for _, f := range r.Variant.Files {
		dest := s.Path(r, f)
		if !s.Installed(r, f) {
			var p Progress
			if newProgress != nil {
				p = newProgress()
			}
			pin := Pin{Size: f.Size, SHA256: f.SHA256}
			if err := d.DownloadPinned(ctx, d.FileURL(r.Variant.Repo, f.Name), dest, pin, p); err != nil {
				return out, err
			}
		}
		switch f.Role {
		case registry.RoleModel:
			out.Model = dest
		case registry.RoleMMProj:
			out.MMProj = dest
		default:
			return out, errs.New(errs.Internal, "unhandled file role %q", f.Role)
		}
	}
	return out, nil
}
