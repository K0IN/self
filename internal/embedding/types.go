// Package embedding defines the typed runtime contract for embedding models.
package embedding

import (
	"context"
	"io"

	"ai-server/internal/settings"
)

type ModelFiles struct {
	Model  string
	MMProj string
}

type Input struct {
	Text    string
	Content []Part `json:"content"`
}

type Part struct {
	Type       string    `json:"type"`
	Text       *string   `json:"text,omitempty"`
	ImageURL   *ImageURL `json:"image_url,omitempty"`
	InputAudio *Audio    `json:"input_audio,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type Audio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
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
	Embed(context.Context, Input) ([]float32, int, error)
	Done() <-chan struct{}
}

type Factory func() Adapter
