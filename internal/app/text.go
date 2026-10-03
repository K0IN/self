package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"ai-server/internal/adapters"
	apitext "ai-server/internal/api/text"
	"ai-server/internal/config"
	"ai-server/internal/errs"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
	"ai-server/internal/text"
)

func serveText(ctx context.Context, cfg config.Serve, t target, out io.Writer, isTTY bool) error {
	engine, err := t.locate(cfg)
	if err != nil {
		return err
	}
	t.describe(out, "")
	files, err := download(ctx, cfg, t.res, out, isTTY)
	if err != nil {
		return err
	}
	entry, err := adapters.Text(t.res.Variant.Adapter)
	if err != nil {
		return err
	}
	adapter := entry.New()
	var engineLog io.Writer
	if cfg.Verbose {
		engineLog = os.Stderr
	}
	fmt.Fprintf(out, "\nUsing %s\n\nLoading model...\n", tildify(files[registry.RoleModel]))
	startCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	startCfg := text.RuntimeConfig{
		ModelID:    t.res.ID(),
		Files:      text.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj]},
		Device:     cfg.Device,
		EnginePath: engine.Path,
		LibDir:     engine.LibDir,
		Log:        engineLog,
		Settings:   t.settings,
	}
	if err := adapter.Start(startCtx, startCfg); err != nil {
		return err
	}
	closeEngine := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = adapter.Close(c)
	}
	info, _ := adapter.Info(ctx)
	if t.res.Model.Capabilities.Input.Has(registry.CapVision) && !info.Vision {
		closeEngine()
		return errs.New(errs.UnsupportedModel, "%s declares vision, but the engine reports no image input (is the mmproj file listed?)", t.res.ID())
	}

	lim := imageutil.DefaultLimits()
	lim.AllowHTTP, lim.AllowPrivate = cfg.AllowHTTPImages, cfg.AllowPrivateImages
	svc := text.NewService(text.ServiceConfig{
		ModelID:               t.res.ID(),
		Quant:                 t.res.Variant.Quant,
		PreprocessConcurrency: cfg.PreprocessConcurrency,
		Info:                  t.res.Model.Info,
		Settings:              t.settings,
	}, adapter, info, imageutil.NewPreprocessor(lim))

	var tail func(int) []string
	if s, ok := adapter.(interface{ StderrTail(int) []string }); ok {
		tail = s.StderrTail
	}
	return serveHTTP(ctx, cfg, out, runningModel{
		Kind:       "text",
		Engine:     engine.Path,
		Device:     info.Device,
		Mount:      apitext.New(svc).Mount,
		Done:       adapter.Done(),
		StderrTail: tail,
		Close:      closeEngine,
	})
}
