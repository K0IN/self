// Package image defines the runtime contract for image generation and editing.
package image

import (
	"context"
	"io"

	"ai-server/internal/settings"
)

type ModelFiles struct {
	Model       string
	VAE         string
	TextEncoder string
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

type GenerateRequest struct {
	Prompt         string
	NegativePrompt string
	N              int
	Width          int
	Height         int
	OutputFormat   string
	Compression    int
	Seed           *int64
	Extra          map[string]any
}

type EditRequest struct {
	Prompt       string
	Images       []InputImage
	Mask         *InputImage
	N            int
	Width        int
	Height       int
	OutputFormat string
	Compression  int
	Extra        map[string]any
}

type InputImage struct {
	Bytes []byte
	MIME  string
	Name  string
}

type Response struct {
	Created      int64
	OutputFormat string
	Images       []OutputImage
}

type OutputImage struct {
	Bytes []byte
	MIME  string
}

type RuntimeInfo struct {
	EngineModel string
	Device      string
}

type Adapter interface {
	Start(context.Context, RuntimeConfig) error
	Close(context.Context) error
	Info(context.Context) (RuntimeInfo, error)
	Generate(context.Context, GenerateRequest) (Response, error)
	Edit(context.Context, EditRequest) (Response, error)
	Done() <-chan struct{}
}

type Factory func() Adapter
