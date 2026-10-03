// Package audio defines the typed runtime contract for speech synthesis.
package audio

import (
	"context"
	"io"

	"ai-server/internal/settings"
)

type ModelFiles struct {
	Model  string
	MMProj string
	Voice  string // optional default reference voice (registry role voice)
}

type RuntimeConfig struct {
	ModelID      string
	Files        ModelFiles
	Instructions bool
	Device       string
	EnginePath   string
	LibDir       string
	Log          io.Writer
	Settings     settings.Values
}

type Voice struct {
	Audio  []byte
	Format string
}

type Request struct {
	Input        string
	Voice        Voice
	Instructions string
	Language     string
	Speed        float64
}

type Response struct {
	WAV        []byte
	SampleRate int
}

type RuntimeInfo struct {
	EngineModel string
	Device      string
	SampleRate  int
}

type Adapter interface {
	Start(context.Context, RuntimeConfig) error
	Close(context.Context) error
	Info(context.Context) (RuntimeInfo, error)
	Synthesize(context.Context, Request) (Response, error)
	Done() <-chan struct{}
}

type Factory func() Adapter
