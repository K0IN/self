package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"ai-server/internal/adapters"
	apiembedding "ai-server/internal/api/embedding"
	"ai-server/internal/config"
	dom "ai-server/internal/embedding"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
)

type embeddingService struct {
	id, quant    string
	info         any
	settings     map[string]any
	adapter      dom.Adapter
	capabilities registry.Capabilities
}

func (s *embeddingService) ModelID() string                     { return s.id }
func (s *embeddingService) Quant() string                       { return s.quant }
func (s *embeddingService) Info() any                           { return s.info }
func (s *embeddingService) Settings() map[string]any            { return s.settings }
func (s *embeddingService) Capabilities() registry.Capabilities { return s.capabilities }
func (s *embeddingService) Embed(ctx context.Context, input dom.Input) ([]float32, int, error) {
	return s.adapter.Embed(ctx, input)
}

func serveEmbedding(ctx context.Context, cfg config.Serve, t target, out io.Writer, isTTY bool) error {
	engine, err := t.locate(cfg)
	if err != nil {
		return err
	}
	t.describe(out, "")
	files, err := download(ctx, cfg, t.res, out, isTTY)
	if err != nil {
		return err
	}
	entry, err := adapters.Embedding(t.res.Variant.Adapter)
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
	if err := adapter.Start(startCtx, dom.RuntimeConfig{ModelID: t.res.ID(), Files: dom.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj]}, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: engineLog, Settings: t.settings}); err != nil {
		return err
	}
	closeEngine := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = adapter.Close(c)
	}
	info, _ := adapter.Info(ctx)
	var tail func(int) []string
	if s, ok := adapter.(interface{ StderrTail(int) []string }); ok {
		tail = s.StderrTail
	}
	svc := &embeddingService{id: t.res.ID(), quant: t.res.Variant.Quant, info: t.res.Model.Info, settings: t.settings, adapter: adapter, capabilities: t.res.Model.Capabilities}
	lim := imageutil.DefaultLimits()
	lim.MaxSourceBytes = 10 << 20
	lim.AllowHTTP, lim.AllowPrivate = cfg.AllowHTTPImages, cfg.AllowPrivateImages
	return serveHTTP(ctx, cfg, out, runningModel{Kind: "embedding", Engine: engine.Path, Device: info.Device, Mount: apiembedding.NewWithFetcher(svc, imageutil.NewFetcher(lim)).Mount, Done: adapter.Done(), StderrTail: tail, Close: closeEngine})
}
