package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"ai-server/internal/adapters"
	apiaudio "ai-server/internal/api/audio"
	dom "ai-server/internal/audio"
	"ai-server/internal/config"
	"ai-server/internal/registry"
)

type audioService struct {
	id, quant string
	info      any
	settings  map[string]any
	adapter   dom.Adapter
}

func (s *audioService) ModelID() string          { return s.id }
func (s *audioService) Quant() string            { return s.quant }
func (s *audioService) Info() any                { return s.info }
func (s *audioService) Settings() map[string]any { return s.settings }
func (s *audioService) Synthesize(ctx context.Context, r dom.Request) (dom.Response, error) {
	return s.adapter.Synthesize(ctx, r)
}

func serveAudio(ctx context.Context, cfg config.Serve, t target, out io.Writer, isTTY bool) error {
	engine, err := t.locate(cfg)
	if err != nil {
		return err
	}
	t.describe(out, "")
	files, err := download(ctx, cfg, t.res, out, isTTY)
	if err != nil {
		return err
	}
	entry, err := adapters.Audio(t.res.Variant.Adapter)
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
	startCfg := dom.RuntimeConfig{ModelID: t.res.ID(), Files: dom.ModelFiles{Model: files[registry.RoleModel], MMProj: files[registry.RoleMMProj], Voice: files[registry.RoleVoice]}, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Log: engineLog, Settings: t.settings}
	if err := adapter.Start(startCtx, startCfg); err != nil {
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
	svc := &audioService{id: t.res.ID(), quant: t.res.Variant.Quant, info: t.res.Model.Info, settings: t.settings, adapter: adapter}
	return serveHTTP(ctx, cfg, out, runningModel{Kind: "audio", Engine: engine.Path, Device: info.Device, Mount: apiaudio.New(svc).Mount, Done: adapter.Done(), StderrTail: tail, Close: closeEngine})
}
