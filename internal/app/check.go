package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"ai-server/internal/config"
	"ai-server/internal/errs"
	"ai-server/internal/registry"
)

// Check onboards a model end to end without HTTP: resolve, download, start
// the engine, send a fixed set of probe questions and verify the answers are
// well-formed and non-degenerate. Used by `self check` and `just check-model`.
func Check(ctx context.Context, cfg config.Serve, out io.Writer, isTTY bool) error {
	t, err := resolveTarget(cfg)
	if err != nil {
		return err
	}
	if t.res.Model.Type != registry.TypeDecision {
		return errs.New(errs.UnsupportedModel, "check supports decision models only")
	}
	engine, err := t.locate(cfg)
	if err != nil {
		return err
	}
	t.describe(out, engine.Path)
	files, err := download(ctx, cfg, t.res, out, isTTY)
	if err != nil {
		return err
	}

	t0 := time.Now()
	de, err := startDecision(ctx, cfg, t, files, engine, nil)
	if err != nil {
		return err
	}
	defer de.close()
	info, caps := de.info, de.caps
	fmt.Fprintf(out, "\nLoaded in %s  (engine model %q, device %s)\n", time.Since(t0).Round(time.Millisecond), info.EngineModel, info.Device)
	fmt.Fprintf(out, "Capabilities  choice=%v score=%v noul=%v vision=%v max_options=%d\n\n", caps.Choice, caps.Score, caps.Noul, caps.Vision, caps.MaxOptions)

	passed, total, err := runProbes(ctx, de.adapter, caps, func(r probeResult) {
		switch {
		case r.skipped:
			fmt.Fprintf(out, "  SKIP  %-28s %s\n", r.name, r.detail)
		case r.err != nil:
			fmt.Fprintf(out, "  FAIL  %-28s %v\n", r.name, r.err)
		default:
			fmt.Fprintf(out, "  %s  %-28s %s  (%s)\n", map[bool]string{true: "ok  ", false: "FAIL"}[r.ok], r.name, r.detail, r.took.Round(time.Millisecond))
		}
	})
	if err != nil {
		return err
	}
	if failed := total - passed; failed > 0 {
		return errs.New(errs.UnsupportedModel, "%d probe(s) failed: the model loads but its answers look wrong for this adapter", failed)
	}
	fmt.Fprintf(out, "\nAll probes passed. %s is ready: self serve %s\n", t.res.ID(), t.res.ID())
	return nil
}
