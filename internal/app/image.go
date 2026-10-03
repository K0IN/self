package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"ai-server/internal/adapters"
	apiimage "ai-server/internal/api/image"
	"ai-server/internal/config"
	dom "ai-server/internal/image"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
)

type imageService struct {
	id, quant string
	info      any
	settings  map[string]any
	adapter   dom.Adapter
}

func (s *imageService) ModelID() string          { return s.id }
func (s *imageService) Quant() string            { return s.quant }
func (s *imageService) Info() any                { return s.info }
func (s *imageService) Settings() map[string]any { return s.settings }
func (s *imageService) Generate(ctx context.Context, req dom.GenerateRequest) (dom.Response, error) {
	return s.adapter.Generate(ctx, req)
}
func (s *imageService) Edit(ctx context.Context, req dom.EditRequest) (dom.Response, error) {
	return s.adapter.Edit(ctx, req)
}

func serveImage(ctx context.Context, cfg config.Serve, t target, out io.Writer, isTTY bool) error {
	engine, err := t.locate(cfg)
	if err != nil {
		return err
	}
	t.describe(out, "")
	files, err := download(ctx, cfg, t.res, out, isTTY)
	if err != nil {
		return err
	}
	entry, err := adapters.Image(t.res.Variant.Adapter)
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
	if err := adapter.Start(startCtx, dom.RuntimeConfig{ModelID: t.res.ID(), Files: dom.ModelFiles{Model: files[registry.RoleModel], VAE: files[registry.RoleVAE], TextEncoder: files[registry.RoleTextEncoder]}, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: engineLog, Settings: t.settings}); err != nil {
		return err
	}
	closeEngine := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = adapter.Close(c)
	}
	info, _ := adapter.Info(ctx)
	lim := imageutil.DefaultLimits()
	lim.AllowHTTP, lim.AllowPrivate = cfg.AllowHTTPImages, cfg.AllowPrivateImages
	svc := &imageService{id: t.res.ID(), quant: t.res.Variant.Quant, info: t.res.Model.Info, settings: t.settings, adapter: adapter}
	var tail func(int) []string
	if s, ok := adapter.(interface{ StderrTail(int) []string }); ok {
		tail = s.StderrTail
	}
	return serveHTTP(ctx, cfg, out, runningModel{Kind: "image", Engine: engine.Path, Device: info.Device, Mount: apiimage.New(svc, lim).Mount, Done: adapter.Done(), StderrTail: tail, Close: closeEngine})
}
