package embedding

import (
	"context"

	"ai-server/internal/adapters/text"
	dom "ai-server/internal/embedding"
	domtext "ai-server/internal/text"
)

type Adapter struct {
	inner *text.Adapter
}

func New() dom.Adapter { return &Adapter{inner: text.New().(*text.Adapter)} }

func (a *Adapter) Start(ctx context.Context, cfg dom.RuntimeConfig) error {
	return a.inner.Start(ctx, domtext.RuntimeConfig{ModelID: cfg.ModelID, Files: domtext.ModelFiles{Model: cfg.Files.Model}, Embedding: true, Pooling: stringSetting(cfg.Settings, "pooling"), Device: cfg.Device, EnginePath: cfg.EnginePath, LibDir: cfg.LibDir, Log: cfg.Log, Settings: cfg.Settings})
}

func stringSetting(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func (a *Adapter) Close(ctx context.Context) error { return a.inner.Close(ctx) }

func (a *Adapter) Info(ctx context.Context) (dom.RuntimeInfo, error) {
	info, err := a.inner.Info(ctx)
	return dom.RuntimeInfo{EngineModel: info.EngineModel, Device: info.Device, ContextSize: info.ContextSize}, err
}

func (a *Adapter) Embed(ctx context.Context, input string) ([]float32, int, error) {
	return a.inner.Embed(ctx, input)
}

func (a *Adapter) Done() <-chan struct{} { return a.inner.Done() }
