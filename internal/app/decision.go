package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"ai-server/internal/adapters"
	apidecision "ai-server/internal/api/decision"
	"ai-server/internal/config"
	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/imageutil"
	"ai-server/internal/models"
	"ai-server/internal/registry"
	"ai-server/internal/runtime"
)

// decisionEngine is a started decision adapter and what it reported.
type decisionEngine struct {
	adapter decision.Adapter
	info    decision.RuntimeInfo
	// caps are the registry capabilities narrowed to what the engine reports.
	caps decision.Capabilities
}

// startDecision starts the adapter of t and checks that the engine delivers
// what the registry promises. log receives engine stderr when non-nil.
func startDecision(ctx context.Context, cfg config.Serve, t target, files models.Files, engine runtime.Engine, log io.Writer) (*decisionEngine, error) {
	entry, err := adapters.Decision(t.res.Variant.Adapter)
	if err != nil {
		return nil, err
	}
	adapter := entry.New()
	startCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err = adapter.Start(startCtx, decision.RuntimeConfig{
		ModelID:    t.res.ID(),
		Files:      decision.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj], Head: files[registry.RoleHead]},
		Device:     cfg.Device,
		EnginePath: engine.Path,
		LibDir:     engine.LibDir,
		Log:        log,
		Settings:   t.settings,
	})
	if err != nil {
		return nil, err
	}
	d := &decisionEngine{adapter: adapter}
	info, err := adapter.Info(ctx)
	if err != nil {
		d.close()
		return nil, err
	}
	d.info = info
	d.caps = modelCaps(t.res).Intersect(info.Capabilities)
	if t.res.Model.Capabilities.Has(registry.CapVision) && !d.caps.Vision {
		d.close()
		return nil, errs.New(errs.UnsupportedModel, "%s declares vision, but the %s engine reports no image input", t.res.ID(), t.res.Variant.Adapter)
	}
	if d.caps.Vision && info.ImageInput == nil {
		d.close()
		return nil, errs.New(errs.RuntimeStartFailed, "engine reports vision support but no image input geometry")
	}
	return d, nil
}

func (d *decisionEngine) close() {
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = d.adapter.Close(c)
}

// modelCaps are the registry capabilities, capped by info.max_options.
func modelCaps(res registry.Resolved) decision.Capabilities {
	c := decision.CapabilitiesFromRegistry(res.Model.Capabilities)
	c.MaxOptions = res.Model.Info.MaxOptions
	return c
}

func serveDecision(ctx context.Context, cfg config.Serve, t target, out io.Writer, isTTY bool) error {
	engine, err := t.locate(cfg)
	if err != nil {
		return err
	}
	t.describe(out, "")
	files, err := download(ctx, cfg, t.res, out, isTTY)
	if err != nil {
		return err
	}

	var engineLog io.Writer
	if cfg.Verbose {
		engineLog = os.Stderr
	}
	fmt.Fprintf(out, "\nUsing %s\n\nLoading model...\n", tildify(files[registry.RoleModel]))
	de, err := startDecision(ctx, cfg, t, files, engine, engineLog)
	if err != nil {
		return err
	}

	lim := imageutil.DefaultLimits()
	lim.AllowHTTP, lim.AllowPrivate = cfg.AllowHTTPImages, cfg.AllowPrivateImages
	svc := decision.NewService(decision.ServiceConfig{
		ModelID:               t.res.ID(),
		Quant:                 t.res.Variant.Quant,
		Capabilities:          de.caps,
		ImageInput:            de.info.ImageInput,
		QueueSize:             cfg.QueueSize,
		PreprocessConcurrency: cfg.PreprocessConcurrency,
		Info:                  t.res.Model.Info,
		Settings:              t.settings,
	}, de.adapter, imageutil.NewPreprocessor(lim))

	var tail func(int) []string
	if s, ok := de.adapter.(interface{ StderrTail(int) []string }); ok {
		tail = s.StderrTail
	}
	return serveHTTP(ctx, cfg, out, runningModel{
		Kind:       "decision",
		Engine:     engine.Path,
		Device:     de.info.Device,
		Mount:      apidecision.New(svc).Mount,
		Done:       de.adapter.Done(),
		StderrTail: tail,
		Fail:       svc.Fail,
		Drain:      svc.Close,
		Close:      de.close,
	})
}
