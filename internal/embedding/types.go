// Package embedding defines the typed runtime contract for embedding models.
package embedding

import (
	"context"
	"io"

	"ai-server/internal/settings"
)

type ModelFiles struct {
	Model string
}

type RuntimeConfig struct {
	ModelID    string
	Files      ModelFiles
	Device     string
	EnginePath string
	LibDir     string
	Log        io.Writer
	Settings   settings.Values
}

type RuntimeInfo struct {
	EngineModel string
	Device      string
	ContextSize int
}

type Adapter interface {
	Start(context.Context, RuntimeConfig) error
	Close(context.Context) error
	Info(context.Context) (RuntimeInfo, error)
	Embed(context.Context, string) ([]float32, int, error)
	Done() <-chan struct{}
}

type Factory func() Adapter
